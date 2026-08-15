package chattui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
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
			Foreground(lipgloss.Color("104"))

	messageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("51"))

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))
)

type screen int

const (
	UsernameScreen screen = iota
	ChatScreen
)

type tabEntry struct {
	label    string
	r        *Room
	messages []ChatMsg
}

type Model struct {
	Sess          *UserSession
	CurrentScreen screen
	UsernameInput textinput.Model
	MessageInput  textinput.Model
	Tabs          []tabEntry
	ActiveTab     int
	UsernameStyle lipgloss.Style
	Width         int
	Height        int
	Err           string
}

func InitialModel(sess *UserSession, width, height int) Model {
	unInput := textinput.New()
	unInput.Placeholder = "enter your username"
	unInput.Focus()
	unInput.CharLimit = 20
	unInput.SetWidth(40)

	msgInput := textinput.New()
	msgInput.Placeholder = "type a message..."
	msgInput.CharLimit = 200
	msgInput.SetWidth(width - 4)

	return Model{
		Sess:          sess,
		CurrentScreen: UsernameScreen,
		UsernameInput: unInput,
		MessageInput:  msgInput,
		Tabs:          []tabEntry{{label: "global", r: nil, messages: []ChatMsg{}}},
		ActiveTab:     0,
		Width:         width,
		Height:        height,
		UsernameStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("141")),
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case ChatMsg:
		for i, t := range m.Tabs {
			if t.r == nil && msg.RoomID == "" {
				m.Tabs[i].messages = append(m.Tabs[i].messages, msg)
				break
			}
			if t.r != nil && t.r.ID == msg.RoomID {
				m.Tabs[i].messages = append(m.Tabs[i].messages, msg)
				break
			}
		}
		return m, nil

	case RoomInviteMsg:
		m.Tabs = append(m.Tabs, tabEntry{label: msg.R.ID, r: msg.R, messages: []ChatMsg{}})
		return m, nil

	case RoomDeleteMsg:
		for i, t := range m.Tabs {
			if t.r == msg.R {
				if m.ActiveTab == i {
					m.ActiveTab = 0
				}
				m.Tabs = slices.Delete(m.Tabs, i, i+1)
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		key := msg.String()

		switch key {
		case "ctrl+c":
			return m, tea.Quit

		case "tab":
			m.ActiveTab = (m.ActiveTab + 1) % len(m.Tabs)
			return m, nil

		case "shift+tab":
			m.ActiveTab = (m.ActiveTab - 1 + len(m.Tabs)) % len(m.Tabs)
			return m, nil

		case "enter":
			if m.CurrentScreen == UsernameScreen {
				username := m.UsernameInput.Value()
				if username == "" {
					return m, nil
				}
				SessionsMu.Lock()
				defer SessionsMu.Unlock()
				_, ok := Sessions[username]
				if ok {
					m.UsernameInput.SetValue("")
					m.Err = "username already taken, try again"
					return m, nil
				}
				m.Err = ""
				m.Sess.Username = username
				m.CurrentScreen = ChatScreen
				m.MessageInput.Focus()
				m.UsernameInput.Blur()
				m.UsernameInput.SetValue("")
				Sessions[username] = m.Sess

				go Broadcast(ChatMsg{
					Text:   fmt.Sprintf("🍄 %s joined the chat", username),
					System: true,
				})
				return m, nil
			}

			if m.CurrentScreen == ChatScreen {
				text := m.MessageInput.Value()
				if text == "" {
					return m, nil
				}

				if strings.HasPrefix(text, "/") {
					parts := strings.SplitN(text, " ", 2)
					command := parts[0]
					args := ""
					if len(parts) > 1 {
						args = parts[1]
						args = strings.TrimSpace(args)
					}

					switch command {
					case "/help":
						m.MessageInput.SetValue("")
						activeRoom := m.Tabs[m.ActiveTab].r
						if activeRoom == nil {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: "", Text: "🍄 slash commands : /help /user /emoji /colors /quit /usercolor COLOR /room RNAME USER1 USER2... /deleteroom", System: true})
						} else {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: activeRoom.ID, Text: "🍄 slash commands : /help /user /emoji /colors /quit /usercolor COLOR /room RNAME USER1 USER2... /deleteroom", System: true})
						}

					case "/user":
						SessionsMu.Lock()
						delete(Sessions, m.Sess.Username)
						SessionsMu.Unlock()
						m.CurrentScreen = UsernameScreen
						m.MessageInput.Blur()
						m.UsernameInput.Focus()
						m.MessageInput.SetValue("")

					case "/emoji":
						m.MessageInput.SetValue("")
						activeRoom := m.Tabs[m.ActiveTab].r
						if activeRoom == nil {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: "", Text: "🍄 Use shortcode notation, or here's emoji's you can access quickly\n😂 😭 ☺️ 🐮 🍄 🤡 🥀 🌈 🔥 🍩 ❤️ ‼️ 👍\nWARNING: DO NOT CTRL+C", System: true})
						} else {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: activeRoom.ID, Text: "🍄 Use shortcode notation, or here's emoji's you can access quickly\n😂 😭 ☺️ 🐮 🍄 🤡 🥀 🌈 🔥 🍩 ❤️ ‼️ 👍\nWARNING: DO NOT CTRL+C", System: true})
						}

					case "/colors":
						m.MessageInput.SetValue("")
						activeRoom := m.Tabs[m.ActiveTab].r
						if activeRoom == nil {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: "", Text: "🍄 available username colors : red blue pink purple", System: true})
						} else {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: activeRoom.ID, Text: "🍄 available username colors : red blue pink purple", System: true})
						}

					case "/usercolor":
						switch args {
						case "red":
							m.UsernameStyle = m.UsernameStyle.Bold(true).Foreground(lipgloss.Color("196"))
						case "blue":
							m.UsernameStyle = m.UsernameStyle.Bold(true).Foreground(lipgloss.Color("51"))
						case "pink":
							m.UsernameStyle = m.UsernameStyle.Bold(true).Foreground(lipgloss.Color("219"))
						case "purple":
							m.UsernameStyle = m.UsernameStyle.Bold(true).Foreground(lipgloss.Color("141"))
						}
						m.MessageInput.SetValue("")
						activeRoom := m.Tabs[m.ActiveTab].r
						if activeRoom == nil {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: "", Text: fmt.Sprintf("🍄 username color changed to : %s", args), System: true})
						} else {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: activeRoom.ID, Text: fmt.Sprintf("🍄 username color changed to : %s", args), System: true})
						}

					case "/room":
						activeRoom := m.Tabs[m.ActiveTab].r
						if activeRoom == nil {
							if args == "" {
								go UserSysMsg(m.Sess, ChatMsg{RoomID: "", Text: "🍄 usage : /room ROOM_NAME USER1 USER2...", System: true})
								m.MessageInput.SetValue("")
								return m, nil
							}
							targetUsernames := strings.Fields(args)
							roomname := targetUsernames[0]
							go CreateRoom(roomname, m.Sess, targetUsernames[1:])
							m.MessageInput.SetValue("")
						} else {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: activeRoom.ID, Text: "🍄 use /room from the global tab to create a new room", System: true})
						}
						m.MessageInput.SetValue("")

					case "/deleteroom":
						activeRoom := m.Tabs[m.ActiveTab].r
						if activeRoom == nil {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: "", Text: "🍄 ainnoway you tryna delete GLOBAL room (p.s. only works inside a room)", System: true})
							m.MessageInput.SetValue("")
							return m, nil
						} else {
							go DeleteRoom(activeRoom)
						}
						m.MessageInput.SetValue("")

					case "/quit":
						return m, tea.Quit

					default:
						activeRoom := m.Tabs[m.ActiveTab].r
						if activeRoom == nil {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: "", Text: "🍄 unknown command : use /help to know more", System: true})
						} else {
							go UserSysMsg(m.Sess, ChatMsg{RoomID: activeRoom.ID, Text: "🍄 unknown command : use /help to know more", System: true})
						}
						m.MessageInput.SetValue("")
					}
					return m, nil
				}

				m.MessageInput.SetValue("")
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

				activeRoom := m.Tabs[m.ActiveTab].r
				if activeRoom == nil {
					go Broadcast(ChatMsg{
						RoomID:   "",
						Username: m.Sess.Username,
						Text:     text,
						System:   false,
					})
				} else {
					go BroadcastToRoom(activeRoom, ChatMsg{
						RoomID:   activeRoom.ID,
						Username: m.Sess.Username,
						Text:     text,
						System:   false,
					})
				}
				return m, nil
			}
		}
	}

	if m.CurrentScreen == UsernameScreen {
		m.UsernameInput, cmd = m.UsernameInput.Update(msg)
	} else {
		m.MessageInput, cmd = m.MessageInput.Update(msg)
	}

	return m, cmd
}

func (m Model) View() tea.View {
	if m.CurrentScreen == UsernameScreen {
		return m.usernameView()
	}
	return m.chatView()
}

func (m Model) usernameView() tea.View {
	welc := headerStyle.Render("Welcome To")
	mussh := musshStyle.Render(`
             ___  ___  _ _                        
 _ _ _  _ _ / __]/ __]| | | _ _  ___  ___  _ _ _  
| ' ' || | |\__ \\__ \|   || '_]/ . \/ . \| ' ' |
|_|_|_| \__|[___/[___/|_|_||_|  \___/\___/|_|_|_|`)
	prompt := welcStyle.Render("🍄 Choose a username to join the chat:")
	s := fmt.Sprintf("\n%s%s\n\n%s\n\n%s\n %s\n", welc, mussh, prompt, m.UsernameInput.View(), m.Err)
	return tea.NewView(s)
}

func (m Model) chatView() tea.View {
	welc := headerStyle.Render("Welcome To")
	mussh := musshStyle.Render(`
             ___  ___  _ _                        
 _ _ _  _ _ / __]/ __]| | | _ _  ___  ___  _ _ _  
| ' ' || | |\__ \\__ \|   || '_]/ . \/ . \| ' ' |
|_|_|_| \__|[___/[___/|_|_||_|  \___/\___/|_|_|_|`)
	welcmsg := welcStyle.Render("🧋 Glad you're here! Use the /help command to know more\n✨ Be respectful, everyone's here to have fun!")
	border := headerStyle.Render("____________________________________________________________")

	var tabBar string
	for i, t := range m.Tabs {
		if i == m.ActiveTab {
			tabBar += tabActiveStyle.Render("["+t.label+"]") + " "
		} else {
			tabBar += tabInactiveStyle.Render("["+t.label+"]") + " "
		}
	}

	var msgLines string
	for _, msg := range m.Tabs[m.ActiveTab].messages {
		if msg.System {
			msgLines += systemStyle.Render(msg.Text) + "\n"
		} else {
			msgLines += m.UsernameStyle.Render(msg.Username+": ") + messageStyle.Render(msg.Text) + "\n"
		}
	}

	help := "/help for commands | enter : send | tab / shift+tab : switch tabs | ctrl+c or /quit : exit chat"
	s := fmt.Sprintf("\n%s%s\n\n%s\n%s\n\n%s\n%s\n\n%s\n%s",
		welc, mussh, welcmsg, border, tabBar, msgLines, m.MessageInput.View(), help)
	return tea.NewView(s)
}
