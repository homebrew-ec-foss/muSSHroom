package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	//UI
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	//wish SSH server packages
	log "charm.land/log/v2"
	wish "charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	"charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"github.com/charmbracelet/ssh"
)

const (
	host = "localhost"
	port = "3000"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("199")) //hot pink

	musshStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("51")) //cyan

	welcStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99")) //purple

	systemStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("104")) //light purple

	messageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")) //white

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("51")) //cyan

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")) //gray

	sessions   = make(map[string]*userSession) //global map of all connected users
	sessionsMu sync.Mutex                      //mutex to protect sessions map from race conditions, ensures each user gets added one by one

	rooms       = make(map[string]*room) //global map of all active rooms
	roomsMu     sync.Mutex               //mutex to protect rooms map from race conditions
	roomCounter int                      //incrementing counter for room IDs
)

// stores each connected user's program reference and username
type userSession struct {
	username string
	program  *tea.Program
}

// room holds the members of a private room
type room struct {
	id      string
	members map[string]*userSession
	mu      sync.Mutex
}

// message type that gets broadcast to all users
type chatMsg struct {
	roomID   string
	username string
	text     string
	system   bool //true if its a system message like "X joined"
}

// roomInviteMsg could be made to send a mesg to users when they are invited, placeholder for now
type roomInviteMsg struct { //this is sent to the update function where it's then appended as the user's tabEntry
	r *room
}

type roomDeleteMsg struct {
	// to send msg to update so that it removes the room from the users' tabs and resets their activeTab to nil for global
	r *room
}

// tabEntry holds a room reference and its message history
type tabEntry struct {
	label    string
	r        *room //nil for global
	messages []chatMsg
}

	// broadcast sends a chatMsg to every connected user's program
	func broadcast(msg chatMsg) {
		sessionsMu.Lock()
		defer sessionsMu.Unlock() //defer waits for the function to finish executing and then executes, even if there's an error
		for _, s := range sessions {
			s.program.Send(msg)
		}
}

// broadcastToRoom sends a chatMsg to every member of a room
func broadcastToRoom(r *room, msg chatMsg) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.members {
		s.program.Send(msg)
	}
}

func userSysMsg(s *userSession, msg chatMsg) { //for when they use slash commands, so that it only appears on their screen
	s.program.Send(msg)
}

// addSession safely adds a new user session to the global map
func addSession(s *userSession) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	sessions[s.username] = s
}

// removeSession safely removes a user session when they disconnect
func removeSession(s *userSession) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	delete(sessions, s.username)
}

// createRoom makes a new room, adds members, and sends roomInviteMsg to each
func createRoom(rname string, creator *userSession, targetUsernames []string) {
	roomsMu.Lock()
	roomCounter++
	id := rname
	r := &room{
		id:      id,
		members: make(map[string]*userSession),
	}
	rooms[id] = r
	roomsMu.Unlock()

	sessionsMu.Lock()
	r.members[creator.username] = creator
	for _, name := range targetUsernames {
		if s, isonline := sessions[name]; isonline {
			r.members[name] = s
		}
	}
	sessionsMu.Unlock()

	r.mu.Lock()
	for _, s := range r.members {
		s.program.Send(roomInviteMsg{r: r})
	}
	r.mu.Unlock()
}

func deleteRoom(r *room) {
	r.mu.Lock()
	for _, s := range r.members {
		s.program.Send(chatMsg{
			roomID: "",
			text:   fmt.Sprintf("🍄 chat room %s deleted", r.id),
			system: true,
		})
		s.program.Send(roomDeleteMsg{r: r}) //sends this room struct to update to make changes in each of their tea.Models
	}
	r.mu.Unlock()

	roomsMu.Lock()
	defer roomsMu.Unlock()
	delete(rooms, r.id) //deletes that room from the rooms map
}

func main() {
	os.Setenv("FORCE_COLOR", "1")

	//setting SSH host key path
	keyPath := os.Getenv("SSH_HOST_KEY_PATH")
	if keyPath == "" {
		keyPath = ".ssh/id_ed25519"
	}

	//setting up the server
	serv, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(keyPath),
		wish.WithMiddleware(
			myMiddleware(),
			activeterm.Middleware(),
			logging.Middleware(),
		),
	)
	if err != nil {
		log.Error("Could not start server", "error", err)
		os.Exit(1)
	}

	//done channel is meant for catching signals to stop the server
	done := make(chan os.Signal, 1)                                    //makes a channel which is an interface to get access to incoming signals
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM) //ctrl+c to end the server , sigint means signal interrupt i.e. ctrl+c
	log.Info("Starting SSH chat server", "host", host, "port", port)   //start up info

	go func() { // error handling for server start up
		if err = serv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil //if it couldn't start the interface is removed, set to null
		}
	}()

	<-done
	log.Info("Stopping SSH chat server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	defer func() { cancel() }()

	if err := serv.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not stop server", "error", err)
	}
}

/*------------------------for EACH user's tea.Program--------------------------------------*/
// myMiddleware uses MiddlewareWithProgramHandler so we get access to *tea.Program pointer
// which we need to call p.Send() for broadcasting messages to each user
func myMiddleware() wish.Middleware {
	teaHandler := func(s ssh.Session) *tea.Program {
		pty, _, active := s.Pty() //sets up the connection for serving bubbletea app on server

		if !active {
			wish.Fatalln(s, "no active terminal, ending")
			return nil
		}

		sess := &userSession{}                                       //create a new user session for this connection
		m := initialModel(sess, pty.Window.Width, pty.Window.Height) //create a new model for this user session
		p := tea.NewProgram(m, bubbletea.MakeOptions(s)...)          //create a new bubbletea program for this user session
		sess.program = p

		//remove session and broadcast disconnect message when user disconnects
		go func() {
			<-s.Context().Done()
			if sess.username != "" {
				broadcast(chatMsg{
					text:   fmt.Sprintf("%s left the chat", sess.username),
					system: true, //to make it a system message
				})
			}
			removeSession(sess) //removes the user from client list
		}()

		return p
	}
	return bubbletea.MiddlewareWithProgramHandler(teaHandler)
}

/*------------------------------------------------------------------------------------*/
//from here we personalise based on our app
//we dont use func main tea.NewProgram here, instead we send Tea.Model to the teaHandler and it then sends
//it to the middleware for it to handle, it's abstracted away
//also styles is stored as a global variable at the top

// screen represents which screen the user is currently on
type screen int

const (
	usernameScreen screen = iota //iota is basically enumeration - 0
	chatScreen                   // 1
)

// model stores the current state of the app for each connected user
type model struct {
	sess          *userSession
	currentScreen screen          //info of which screen the user is on, user or chat screen (use in rooms later)
	usernameInput textinput.Model //input box for username entry
	messageInput  textinput.Model //input box for chat messages
	tabs          []tabEntry      //amount of tabs
	activeTab     int             //current tab
	usernameStyle lipgloss.Style
	width         int
	height        int
	err           string //error message for username taken
}

func initialModel(sess *userSession, width, height int) model { //model state when user first enters in

	//username input setup
	unInput := textinput.New()
	unInput.Placeholder = "enter your username"
	unInput.Focus()
	unInput.CharLimit = 20
	unInput.SetWidth(40)

	//message input setup
	msgInput := textinput.New()
	msgInput.Placeholder = "type a message..."
	msgInput.CharLimit = 200
	msgInput.SetWidth(width - 4)

	return model{
		sess:          sess,
		currentScreen: usernameScreen,
		usernameInput: unInput,
		messageInput:  msgInput,
		tabs:          []tabEntry{{label: "global", r: nil, messages: []chatMsg{}}},
		activeTab:     0,
		width:         width,
		height:        height,
		usernameStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("141")),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg: //update dimensions if terminal is resized
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case chatMsg: //a broadcast message arrived, append to correct tab's history
		for i, t := range m.tabs {
			if t.r == nil && msg.roomID == "" {
				m.tabs[i].messages = append(m.tabs[i].messages, msg)
				break
			}
			if t.r != nil && t.r.id == msg.roomID {
				m.tabs[i].messages = append(m.tabs[i].messages, msg)
				break
			}
		}
		return m, nil

	case roomInviteMsg: //a new room was created and this user is also a member
		m.tabs = append(m.tabs, tabEntry{label: msg.r.id, r: msg.r, messages: []chatMsg{}})
		return m, nil

	case roomDeleteMsg:
		for i, t := range m.tabs { //iterates through tabs to find the tabEntry to be deleted
			if t.r == msg.r { //checks which tab contains the room being deleted
				if m.activeTab == i {
					m.activeTab = 0 //sends them back to global room if they're currently in the room being deleted
				}
				m.tabs = slices.Delete(m.tabs, i, i+1) //deletes the tabEntry from m.tabs
			}

		}
		return m, nil

	case tea.KeyPressMsg:
		key := msg.String()

		switch key {
		case "ctrl+c":
			return m, tea.Quit

		case "tab": //go to next tab
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
			return m, nil

		case "shift+tab": //back to previous tab
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			return m, nil

		case "enter":
			if m.currentScreen == usernameScreen { //i.e. screen where they enter username
				username := m.usernameInput.Value()
				if username == "" {
					return m, nil
				}
				sessionsMu.Lock()
				defer sessionsMu.Unlock()   //unlock after this function is done
				_, ok := sessions[username] //ok returns true if that username is taken
				if ok {                     //if username already exists
					m.usernameInput.SetValue("")
					m.err = "username already taken, try again" //since there is no tea program made yet for new users cant use sendmesg
					return m, nil
				}
				m.err = "" //clear error mesg if username proceeds smoothly

				//set username on the session so broadcast can use it
				m.sess.username = username
				m.currentScreen = chatScreen //take them to the chat screen
				m.messageInput.Focus()       //taking cursor to chat and away from username input text box
				m.usernameInput.Blur()
				m.usernameInput.SetValue("")
				sessions[username] = m.sess

				//broadcast join message to everyone
				go broadcast(chatMsg{ //goroutine
					text:   fmt.Sprintf("🍄 %s joined the chat", username),
					system: true,
				})
				return m, nil
			}

			if m.currentScreen == chatScreen { //if user is on chatscreen and hit enter
				text := m.messageInput.Value()
				if text == "" { //if input is nothing skip and do nothing
					return m, nil
				}

				// detecting slash commands
				if strings.HasPrefix(text, "/") {
					parts := strings.SplitN(text, " ", 2) //splits it into "/command" and "args"
					command := parts[0]                   //name of command like 'help'
					args := ""

					if len(parts) > 1 { //i.e the args to the commands like COLOR in /usercolor
						args = parts[1]
					}

					switch command { //to see which command is entered and send a user sysmsg to their session accordingly

					case "/help":
						m.messageInput.SetValue("")
						activeRoom := m.tabs[m.activeTab].r
						if activeRoom == nil {
							//this cases means that user is on the global room, sysmsg goes there
							go userSysMsg(m.sess, chatMsg{
								roomID: "",
								text:   "🍄 slash commands : /help /user /emoji /colors /quit /usercolor COLOR /room RNAME USER1 USER2... /deleteroom",
								system: true})
						} else {
							//broadcast the sys message to user in their respective room
							go userSysMsg(m.sess, chatMsg{
								roomID: activeRoom.id,
								text:   "🍄 slash commands : /help /user /emoji /colors /quit /usercolor COLOR /room RNAME USER1 USER2... /deleteroom",
								system: true})
						}

					case "/user":
						sessionsMu.Lock()
						delete(sessions, m.sess.username) //removes the current name from sessions
						sessionsMu.Unlock()
						m.currentScreen = usernameScreen //switches to username screen
						m.messageInput.Blur()
						m.usernameInput.Focus()
						m.messageInput.SetValue("")

					case "/emoji":
						m.messageInput.SetValue("")
						activeRoom := m.tabs[m.activeTab].r
						if activeRoom == nil {
							//this cases means that user is on the global room, sysmsg goes there
							go userSysMsg(m.sess, chatMsg{ //broadcasts a system message only to user
								roomID: "",
								text: `
🍄 Use shortcode notation, or here's emoji's you can access quickly, first select the one you want
ctrl+shift+c --> ctrl+shift+v into your message box
😂 😭 ☺️ 🐮 🍄 🤡 🥀 🌈 🔥 🍩 ❤️ ‼️ 👍
WARNING: DO NOT CTRL+C`,
								system: true})
						} else {
							//broadcast the sys message to user in their respective room
							go userSysMsg(m.sess, chatMsg{ //broadcasts a system message only to user
								roomID: activeRoom.id,
								text: `
🍄 Use shortcode notation, or here's emoji's you can access quickly, first select the one you want
ctrl+shift+c --> ctrl+shift+v into your message box
😂 😭 ☺️ 🐮 🍄 🤡 🥀 🌈 🔥 🍩 ❤️ ‼️ 👍
WARNING: DO NOT CTRL+C`,
								system: true})
						}

					case "/colors":
						m.messageInput.SetValue("")
						activeRoom := m.tabs[m.activeTab].r
						if activeRoom == nil {
							//this cases means that user is on the global room, sysmsg goes there
							go userSysMsg(m.sess, chatMsg{
								roomID: "",
								text:   "🍄 available username colors : red blue pink purple",
								system: true})
						} else {
							//broadcast the sys message to user in their respective room
							go userSysMsg(m.sess, chatMsg{
								roomID: activeRoom.id,
								text:   "🍄 available username colors : red blue pink purple",
								system: true})
						}

					case "/usercolor":

						switch args {

						case "red":
							m.usernameStyle = m.usernameStyle.Bold(true).Foreground(lipgloss.Color("196")) //red

						case "blue":
							m.usernameStyle = m.usernameStyle.Bold(true).Foreground(lipgloss.Color("51")) //cyan

						case "pink":
							m.usernameStyle = m.usernameStyle.Bold(true).Foreground(lipgloss.Color("219")) //pink

						case "purple":
							m.usernameStyle = m.usernameStyle.Bold(true).Foreground(lipgloss.Color("141")) //purple
						}
						m.messageInput.SetValue("")

						activeRoom := m.tabs[m.activeTab].r
						if activeRoom == nil {
							//this cases means that user is on the global room, sysmsg goes there
							go userSysMsg(m.sess, chatMsg{
								roomID: "",
								text:   fmt.Sprintf("🍄 username color changed to : %s", args),
								system: true})
						} else {
							//broadcast the sys message to user in their respective room
							go userSysMsg(m.sess, chatMsg{
								roomID: activeRoom.id,
								text:   fmt.Sprintf("🍄 username color changed to : %s", args),
								system: true})
						}

					case "/room":
						activeRoom := m.tabs[m.activeTab].r
						if activeRoom == nil {
							//this cases means that user is on the global room
							if args == "" { //no args given
								go userSysMsg(m.sess, chatMsg{
									roomID: "",
									text:   "🍄 usage : /room USER1 USER2...",
									system: true})
								m.messageInput.SetValue("")
								return m, nil
							}
							//creates room if args are valid users
							targetUsernames := strings.Fields(args)
							roomname := targetUsernames[0]
							go createRoom(roomname, m.sess, targetUsernames[1:])
							m.messageInput.SetValue("")

						} else { //incase it's inside a room we dont want them to create a room from here for neatness purpose lmao
							//broadcast the sys message to user in their respective room
							go userSysMsg(m.sess, chatMsg{
								roomID: activeRoom.id,
								text:   "🍄 use /room from the global tab to create a new room",
								system: true})
						}
						m.messageInput.SetValue("")

					case "/deleteroom":
						activeRoom := m.tabs[m.activeTab].r
						if activeRoom == nil {
							//this cases means that user is on the global room
							if args == "" { //no args given
								go userSysMsg(m.sess, chatMsg{
									roomID: "",
									text:   "🍄 ainnoway you tryna delete GLOBAL room (p.s. only works inside a room)",
									system: true})
								m.messageInput.SetValue("")
								return m, nil
							}

						} else { //incase it's inside a room we dont want them to create a room from here for neatness purpose lmao
							//broadcast the sys message to user in their respective room
							go deleteRoom(activeRoom)

						}
						m.messageInput.SetValue("")

					case "/quit":
						return m, tea.Quit

					default:
						activeRoom := m.tabs[m.activeTab].r
						if activeRoom == nil {
							//this cases means that user is on the global room, sysmsg goes there
							go userSysMsg(m.sess, chatMsg{
								roomID: "",
								text:   "🍄 unknown command : use /help to know more",
								system: true})
						} else {
							//broadcast the sys message to user in their respective room
							go userSysMsg(m.sess, chatMsg{
								roomID: activeRoom.id,
								text:   "🍄 unknown command : use /help to know more",
								system: true})
						}
						m.messageInput.SetValue("")
					}

					return m, nil
				}

				m.messageInput.SetValue("") //once message broadcasted set the box empty
				text = strings.ReplaceAll(text, ":sob:", "😭")
				text = strings.ReplaceAll(text, ":joy:", "😂")
				text = strings.ReplaceAll(text, ":relaxed:", "☺️")
				text = strings.ReplaceAll(text, ":cow:", "🐮")
				text = strings.ReplaceAll(text, ":mushroom:", "🍄")
				text = strings.ReplaceAll(text, ":clown:", "🤡")
				text = strings.ReplaceAll(text, ":wilted_flower:", "🥀")
				text = strings.ReplaceAll(text, ":wilted_rose:", "🥀")
				text = strings.ReplaceAll(text, ":rainbow:", "🌈")
				text = strings.ReplaceAll(text, ":fire:", "🔥")
				text = strings.ReplaceAll(text, ":doughnut:", "🍩")
				text = strings.ReplaceAll(text, ":heart:", "❤️")
				text = strings.ReplaceAll(text, ":bangbang:", "‼️")
				text = strings.ReplaceAll(text, ":thumbsup:", "👍")
				text = strings.ReplaceAll(text, ":+1:", "👍")
				activeRoom := m.tabs[m.activeTab].r
				if activeRoom == nil {
					//this cases means that user wants to send a global mesg
					go broadcast(chatMsg{ //goroutine to broadcast the message to all channels
						roomID:   "",
						username: m.sess.username,
						text:     text,
						system:   false,
					})
				} else {
					//broadcast the message to room members only
					go broadcastToRoom(activeRoom, chatMsg{
						roomID:   activeRoom.id,
						username: m.sess.username,
						text:     text,
						system:   false,
					})
				}
				return m, nil
			}
		}
	}

	//route input updates to the correct input box based on current screen
	if m.currentScreen == usernameScreen {
		m.usernameInput, cmd = m.usernameInput.Update(msg)
	} else {
		m.messageInput, cmd = m.messageInput.Update(msg)
	}

	return m, cmd
}

func (m model) View() tea.View {
	if m.currentScreen == usernameScreen {
		return m.usernameView()
	}
	return m.chatView()
}

// usernameView is the first screen shown when a user connects
func (m model) usernameView() tea.View {
	welc := headerStyle.Render("Welcome To")
	mussh := musshStyle.Render(`
             ___  ___  _ _                        
 _ _ _  _ _ / __]/ __]| | | _ _  ___  ___  _ _ _  
| ' ' || | |\__ \\__ \|   || '_]/ . \/ . \| ' ' |
|_|_|_| \__|[___/[___/|_|_||_|  \___/\___/|_|_|_|`)

	prompt := welcStyle.Render("🍄 Choose a username to join the chat:")
	s := fmt.Sprintf("\n%s%s\n\n%s\n\n%s\n %s\n", welc, mussh, prompt, m.usernameInput.View(), m.err)
	return tea.NewView(s)
}

// chatView is the main chat screen shown after username is set
func (m model) chatView() tea.View {
	welc := headerStyle.Render("Welcome To")
	mussh := musshStyle.Render(`
             ___  ___  _ _                        
 _ _ _  _ _ / __]/ __]| | | _ _  ___  ___  _ _ _  
| ' ' || | |\__ \\__ \|   || '_]/ . \/ . \| ' ' |
|_|_|_| \__|[___/[___/|_|_||_|  \___/\___/|_|_|_|`)

	welcmsg := welcStyle.Render("🧋 Glad you're here! Use the /help command to know more\n✨ Be respectful, everyone's here to have fun!")
	border := headerStyle.Render("____________________________________________________________")

	//render tabs
	var tabBar string
	for i, t := range m.tabs {
		if i == m.activeTab {
			tabBar += tabActiveStyle.Render("["+t.label+"]") + " "
		} else {
			tabBar += tabInactiveStyle.Render("["+t.label+"]") + " "
		}
	}

	//render message history
	var msgLines string
	for _, msg := range m.tabs[m.activeTab].messages {
		if msg.system {
			msgLines += systemStyle.Render(msg.text) + "\n"
		} else {
			msgLines += m.usernameStyle.Render(msg.username+": ") + messageStyle.Render(msg.text) + "\n"
		}
	}

	help := "/help for commands | enter : send | tab / shift+tab : switch tabs | ctrl+c or /quit : exit chat"

	s := fmt.Sprintf("\n%s%s\n\n%s\n%s\n\n%s\n%s\n\n%s\n%s",
		welc, mussh, welcmsg, border, tabBar, msgLines, m.messageInput.View(), help)
	return tea.NewView(s)
}
