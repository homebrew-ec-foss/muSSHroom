package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
  "sync"
	"os/signal"
	"syscall"
	"time"

	//UI
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
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
			Foreground(lipgloss.Color("199"))

	musshStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("51"))

	welcStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99"))

	systemStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214"))

	messageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	usernameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("141"))

	sessions   = make([]*userSession, 0) //global slice of all connected users
	sessionsMu sync.Mutex                //mutex to protect sessions slice from race conditions
)

// stores each connected user's program reference and username
type userSession struct {
	username string
	program  *tea.Program
}

//  message type that gets broadcast to all users
type chatMsg struct {
	username string
	text     string
	system   bool //true if its a system message like "X joined"
}

// broadcast sends a chatMsg to every connected user's program
func broadcast(msg chatMsg) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	for _, s := range sessions {
		s.program.Send(msg)
	}
}

// addSession safely adds a new user session to the global slice
func addSession(s *userSession) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	sessions = append(sessions, s)
}

// removeSession safely removes a user session when they disconnect
func removeSession(s *userSession) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	for i, sess := range sessions {
		if sess == s {
			sessions = append(sessions[:i], sessions[i+1:]...) //removes that session by ignoring that particular index of i
			return
		}
	}
}

func main() {
	keyPath := os.Getenv("SSH_HOST_KEY_PATH")  
	if keyPath == "" {
		keyPath = ".ssh/id_ed25519"
	}

	s, err := wish.NewServer(
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
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)  //ctrl+c to end the server
	log.Info("Starting SSH chat server", "host", host, "port", port)    //start up info

	go func() {           // error handling for server start up
		if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done
	log.Info("Stopping SSH chat server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not stop server", "error", err)
	}
}

// myMiddleware uses MiddlewareWithProgramHandler so we get access to *tea.Program
// which we need to call p.Send() for broadcasting messages to each user
func myMiddleware() wish.Middleware {
	teaHandler := func(s ssh.Session) *tea.Program {
		pty, _, active := s.Pty()
		if !active {
			wish.Fatalln(s, "no active terminal, ending")
			return nil
		}

		sess := &userSession{}                               //create a new user session for this connection
		m := initialModel(sess, pty.Window.Width, pty.Window.Height) //create a new model for this user session
		p := tea.NewProgram(m, bubbletea.MakeOptions(s)...)    //create a new bubbletea program for this user session
		sess.program = p 

		//add session to global slice when user connects
		addSession(sess)

		//remove session and broadcast disconnect message when user disconnects
		go func() {
			<-s.Context().Done()
			if sess.username != "" {
				broadcast(chatMsg{
					text:   fmt.Sprintf("%s left the chat", sess.username),
					system: true,
				})
			}
			removeSession(sess)
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
	usernameScreen screen = iota //iota is basically enumaration
	chatScreen                   //
)

// model stores the current state of the app for each connected user
type model struct {
	sess          *userSession
	currentScreen screen
	usernameInput textinput.Model //input box for username entry
	messageInput  textinput.Model //input box for chat messages
	messages      []chatMsg       //history of all received messages
	width         int
	height        int
}

func initialModel(sess *userSession, width, height int) model {
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
		messages:      []chatMsg{},
		width:         width,
		height:        height,
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

	case chatMsg: //a broadcast message arrived, append to history
		m.messages = append(m.messages, msg)
		return m, nil

	case tea.KeyPressMsg:
		key := msg.String()

		switch key {
		case "ctrl+c":
			return m, tea.Quit

		case "enter":
			if m.currentScreen == usernameScreen {
				username := m.usernameInput.Value()
				if username == "" {
					return m, nil
				}
				//set username on the session so broadcast can use it
				m.sess.username = username
				m.currentScreen = chatScreen
				m.messageInput.Focus()
				m.usernameInput.Blur()  
				//broadcast join message to everyone
				go broadcast(chatMsg{
					text:   fmt.Sprintf("%s joined the chat", username),
					system: true,
				})
				return m, nil
			}

			if m.currentScreen == chatScreen {
				text := m.messageInput.Value()
				if text == "" {                                //if input is nothing skip
					return m, nil
				}
				m.messageInput.SetValue("")
				//broadcast the message to all users
				go broadcast(chatMsg{
					username: m.sess.username,            
					text:     text,
					system:   false,
				})
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

	prompt := welcStyle.Render("Choose a username to join the chat:")
	s := fmt.Sprintf("\n%s%s\n\n%s\n\n%s\n", welc, mussh, prompt, m.usernameInput.View())
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

	//render message history
	var msgLines string
	for _, msg := range m.messages {
		if msg.system {
			msgLines += systemStyle.Render("* "+msg.text) + "\n"
		} else {
			msgLines += usernameStyle.Render(msg.username+": ") + messageStyle.Render(msg.text) + "\n"
		}
	}

	help := "enter : send | ctrl+c : quit"

	s := fmt.Sprintf("\n%s%s\n\n%s\n%s\n\n%s\n%s\n%s",
		welc, mussh, welcmsg, border, msgLines, m.messageInput.View(), help)
	return tea.NewView(s)
}
