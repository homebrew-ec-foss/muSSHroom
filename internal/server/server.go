package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	log "charm.land/log/v2"
	wish "charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	"charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"github.com/charmbracelet/ssh"
	chattui "github.com/homebrew-ec-foss/muSSHroom/internal/chat-tui"
)

const (
	Host = "localhost"
	Port = "3000"
)

func Start() {
	keyPath := os.Getenv("SSH_HOST_KEY_PATH")
	if keyPath == "" {
		keyPath = ".ssh/id_ed25519"
	}

	serv, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(Host, Port)),
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

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Info("Starting SSH chat server", "host", Host, "port", Port)

	go func() {
		if err = serv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done
	log.Info("Stopping SSH chat server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := serv.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not stop server", "error", err)
	}
}

func myMiddleware() wish.Middleware {
	teaHandler := func(s ssh.Session) *tea.Program {
		pty, _, active := s.Pty()
		if !active {
			wish.Fatalln(s, "no active terminal, ending")
			return nil
		}

		sess := &chattui.UserSession{}
		m := chattui.InitialModel(sess, pty.Window.Width, pty.Window.Height)
		p := tea.NewProgram(m, bubbletea.MakeOptions(s)...)
		sess.Program = p

		go func() {
			<-s.Context().Done()
			if sess.Username != "" {
				chattui.Broadcast(chattui.ChatMsg{
					Text:   fmt.Sprintf("%s left the chat", sess.Username),
					System: true,
				})
			}
			chattui.RemoveSession(sess)
		}()

		return p
	}
	return bubbletea.MiddlewareWithProgramHandler(teaHandler)
}
