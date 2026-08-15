package chattui

import (
	"fmt"
	"sync"

	tea "charm.land/bubbletea/v2"
)

var (
	Sessions    = make(map[string]*UserSession)
	SessionsMu  sync.Mutex
	Rooms       = make(map[string]*Room)
	RoomsMu     sync.Mutex
	RoomCounter int
)

type ChatMsg struct {
	RoomID   string
	Username string
	Text     string
	System   bool
}

type RoomInviteMsg struct {
	R *Room
}

type RoomDeleteMsg struct {
	R *Room
}

type UserSession struct {
	Username string
	Program  *tea.Program
}

type Room struct {
	ID      string
	Members map[string]*UserSession
	Mu      sync.Mutex
}

func AddSession(s *UserSession) {
	SessionsMu.Lock()
	defer SessionsMu.Unlock()
	Sessions[s.Username] = s
}

func RemoveSession(s *UserSession) {
	SessionsMu.Lock()
	defer SessionsMu.Unlock()
	delete(Sessions, s.Username)
}

func Broadcast(msg ChatMsg) {
	SessionsMu.Lock()
	defer SessionsMu.Unlock()
	for _, s := range Sessions {
		s.Program.Send(msg)
	}
}

func BroadcastToRoom(r *Room, msg ChatMsg) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for _, s := range r.Members {
		s.Program.Send(msg)
	}
}

func UserSysMsg(s *UserSession, msg ChatMsg) {
	s.Program.Send(msg)
}

func CreateRoom(rname string, creator *UserSession, targetUsernames []string) {
	RoomsMu.Lock()
	RoomCounter++
	r := &Room{
		ID:      rname,
		Members: make(map[string]*UserSession),
	}
	Rooms[rname] = r
	RoomsMu.Unlock()

	SessionsMu.Lock()
	r.Members[creator.Username] = creator
	for _, name := range targetUsernames {
		if s, online := Sessions[name]; online {
			r.Members[name] = s
		}
	}
	SessionsMu.Unlock()

	r.Mu.Lock()
	for _, s := range r.Members {
		s.Program.Send(RoomInviteMsg{R: r})
	}
	r.Mu.Unlock()
}

func DeleteRoom(r *Room) {
	r.Mu.Lock()
	for _, s := range r.Members {
		s.Program.Send(ChatMsg{
			RoomID: "",
			Text:   fmt.Sprintf("🍄 chat room %s deleted", r.ID),
			System: true,
		})
		s.Program.Send(RoomDeleteMsg{R: r})
	}
	r.Mu.Unlock()

	RoomsMu.Lock()
	defer RoomsMu.Unlock()
	delete(Rooms, r.ID)
}
