package skidd

import (
	"fmt"
	"log"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

// ErrNilServerConfig is returned when a ConnMaster has no server config.
var ErrNilServerConfig = fmt.Errorf("Nil server config")

// ConnMasterConn is an struct used to store the net.Conn and ssh.ServerConn
// for a connection.
type ConnMasterConn struct {
	conn       net.Conn
	serverConn *ssh.ServerConn
}

// ConnMaster is used to manage a set of net.Conn and ssh.ServerConn
// connections.
type ConnMaster struct {
	sync.RWMutex
	conns         map[string]*ConnMasterConn
	readCallbacks map[string]map[string]func(string) error
	serverConfig  *ssh.ServerConfig
}

// NewConnMaster returns a *ConnMaster.
func NewConnMaster(serverConfig *ssh.ServerConfig) *ConnMaster {
	return &ConnMaster{
		conns:         map[string]*ConnMasterConn{},
		serverConfig:  serverConfig,
		readCallbacks: make(map[string]map[string]func(string) error),
	}
}

func (cm *ConnMaster) RegisterReadCallback(channel, key string, cb func(string) error) {
	cm.Lock()
	_, ok := cm.readCallbacks[channel]
	if !ok {
		cm.readCallbacks[channel] = map[string]func(string) error{key: cb}
		cm.Unlock()
		return
	}
	cm.readCallbacks[channel][key] = cb
	cm.Unlock()
}

func (cm *ConnMaster) Callbacks(channel string) []func(string) error {
	cm.RLock()
	cbMap := cm.readCallbacks[channel]
	cbs := []func(string) error{}
	for _, cb := range cbMap {
		cbs = append(cbs, cb)
	}
	cm.RUnlock()

	return cbs
}

func (cm *ConnMaster) OnRead(channel, payload string) {
	cm.RLock()
	cbMap := cm.readCallbacks[channel]
	cm.RUnlock()

	for key, cb := range cbMap {
		if err := cb(payload); err != nil {
			cm.Lock()
			delete(cm.readCallbacks[channel], key)
			cm.Unlock()
		}
	}
}

// Conns returns the map of active connections.
func (cm *ConnMaster) Conns() map[string]*ConnMasterConn {
	cm.RLock()
	conns := cm.conns
	cm.RUnlock()

	return conns
}

// Conns returns the map of active connections.
func (cm *ConnMaster) ConnKeys() []string {
	cm.RLock()
	conns := cm.conns
	cm.RUnlock()

	keys := []string{}
	for key, _ := range conns {
		keys = append(keys, key)
	}

	return keys
}

// ConnKey returns a string that is use for keeping track of the conn.
func (cm ConnMaster) ConnKey(conn net.Conn) string {
	remoteAddr := conn.RemoteAddr()

	return fmt.Sprintf(
		"%s:%s",
		remoteAddr.Network(),
		remoteAddr.String(),
	)
}

//
func (cm *ConnMaster) AddConn(conn net.Conn) {
	key := cm.ConnKey(conn)

	cm.Lock()
	cmConn := cm.conns[key]
	if cmConn == nil {
		cm.conns[key] = &ConnMasterConn{
			conn: conn,
		}
	} else {
		cmConn.conn = conn
		cm.conns[key] = cmConn
	}
	cm.Unlock()
}

func (cm *ConnMaster) AddServerConn(
	key string,
	serverConn *ssh.ServerConn,
) {
	cm.Lock()
	cmConn := cm.conns[key]
	if cmConn == nil {
		cm.conns[key] = &ConnMasterConn{
			serverConn: serverConn,
		}
	} else {
		cmConn.serverConn = serverConn
		cm.conns[key] = cmConn
	}
	cm.Unlock()
}

func (cm *ConnMaster) RemoveConn(key string) {
	cm.Lock()
	delete(cm.conns, key)
	cm.Unlock()
}

func (cm *ConnMaster) ServerConfig() *ssh.ServerConfig {
	cm.RLock()
	serverConfig := cm.serverConfig
	cm.RUnlock()

	return serverConfig
}

func (cm *ConnMaster) HandleConn(conn net.Conn) error {
	cm.AddConn(conn)
	key := cm.ConnKey(conn)
	defer cm.RemoveConn(key)

	log.Printf("New skidd on the block: %s\n", key)

	serverConfig := cm.ServerConfig()
	if serverConfig == nil {
		return ErrNilServerConfig
	}

	serverConn, newChans, reqChan, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		return err
	}

	cm.AddServerConn(key, serverConn)

	// The incoming Request channel must be serviced.
	go ssh.DiscardRequests(reqChan)

	for newChan := range newChans {
		// Channels have a type, depending on the application level
		// protocol intended. In the case of a shell, the type is
		// "session" and ServerShell may be used to present a simple
		// terminal interface.
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChan.Accept()
		if err != nil {
			log.Printf("Could not accept channel: %v\n", err)
			return err
		}

		// Sessions have out-of-band requests such as "shell",
		// "pty-req" and "env".  Here we handle only the
		// "shell" request.
		go func(in <-chan *ssh.Request) {
			for req := range in {
				fmt.Printf("%+v\n", req)
				req.Reply(req.Type == "shell" || req.Type == "pty-req" || req.Type == "env", nil)
			}
		}(requests)

		term := terminal.NewTerminal(channel, "> ")

		go func(key string) {
			defer channel.Close()
			for {
				line, err := term.ReadLine()
				if err != nil {
					log.Println(err)
					break
				}

				fmt.Println(line)
				cm.OnRead(key, line)
			}
		}(key)
	}

	return nil
}
