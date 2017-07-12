package skidd

import (
	"fmt"
	_ "io"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Upgrader is a struct that contains ws protocol upgrade configuration.
var Upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WsMsg is used to communicate with websocket clients
type WsMsg struct {
	Channels []string `json:"channels,omitempty"`
	Channel  string   `json:"channel,omitempty"`
	Command  string   `json:"command,omitempty"`
	Error    string   `json:"error,omitempty"`
	Payload  string   `json:"payload,omitempty"`
}

// WsConnMaster is used to manage a set of websocket.Conns
type WsConnMaster struct {
	sync.RWMutex
	wsConns map[string]*websocket.Conn
	cm      *ConnMaster
}

// NewWsConnMaster returns a *WsConnMaster
func NewWsConnMaster(cm *ConnMaster) *WsConnMaster {
	return &WsConnMaster{
		wsConns: map[string]*websocket.Conn{},
		cm:      cm,
	}
}

func (wsCm *WsConnMaster) WsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	if err := wsCm.HandleWsConn(conn); err != nil {
		log.Println(err)
		return
	}
}

// WsConnKey returns the key for the *websocket.Conn
func (wsCm WsConnMaster) WsConnKey(wsConn *websocket.Conn) string {
	if wsConn == nil {
		return ""
	}

	remoteAddr := wsConn.RemoteAddr()

	return fmt.Sprintf(
		"%s:%s",
		remoteAddr.Network(),
		remoteAddr.String(),
	)
}

// AddWsConn adds a *websocket.Conn to the WsConnMaster.
func (wsCm *WsConnMaster) AddWsConn(wsConn *websocket.Conn) {
	if wsConn == nil {
		return
	}

	key := wsCm.WsConnKey(wsConn)
	wsConn.SetPingHandler(wsCm.PingPongHandler)
	wsConn.SetPongHandler(wsCm.PingPongHandler)

	wsCm.Lock()
	wsCm.wsConns[key] = wsConn
	wsCm.Unlock()
}

// RemoveWsConn removes a connection from the WsConnMaster.
func (wsCm *WsConnMaster) RemoveWsConn(key string) {
	wsCm.Lock()
	delete(wsCm.wsConns, key)
	wsCm.Unlock()
}

// PingPongHandler is a noop response handler for websocket connections.
func (wsCm *WsConnMaster) PingPongHandler(data string) error {
	println("ping pong")
	return nil
}

// HandleWsConn handles the *websocket.Conn
func (wsCm *WsConnMaster) HandleWsConn(wsConn *websocket.Conn) error {
	key := wsCm.WsConnKey(wsConn)
	log.Printf("Adding Ws Conn: %s\n", key)

	wsCm.AddWsConn(wsConn)
	defer wsCm.RemoveWsConn(key)

	for {
		var wsMsg WsMsg

		if err := wsConn.ReadJSON(&wsMsg); err != nil {
			fmt.Println(err)
			log.Println(err)
			break
		}

		switch wsMsg.Command {
		case "list":
			if err := wsConn.WriteJSON(wsCm.OnList()); err != nil {
				log.Println(err)
				break
			}

		case "sub":
			resMsg := wsCm.OnSub(key, wsMsg.Payload, wsConn)
			if err := wsConn.WriteJSON(resMsg); err != nil {
				log.Println(err)
				break
			}

		default:
			if err := wsConn.WriteJSON(wsMsg); err != nil {
				log.Println(err)
				break
			}
		}
	}

	return nil
}

func (wsCm *WsConnMaster) OnList() *WsMsg {
	wsCm.RLock()
	cm := wsCm.cm
	wsCm.RUnlock()

	if cm == nil {
		return &WsMsg{
			Command: "list",
			Error:   "no channels",
		}
	}

	channels := cm.ConnKeys()
	if len(channels) == 0 {
		return &WsMsg{
			Command: "list",
			Error:   "no channels",
		}
	}

	return &WsMsg{
		Command:  "list",
		Channels: cm.ConnKeys(),
	}
}

func (wsCm *WsConnMaster) OnSub(
	key, channel string,
	wsConn *websocket.Conn,
) *WsMsg {
	wsCm.Lock()
	cm := wsCm.cm
	wsCm.Unlock()
	if cm == nil {
		return &WsMsg{
			Command: "sub",
			Error:   "no connections",
		}
	}

	cm.RegisterReadCallback(
		channel,
		key,
		func(s string) error {
			return wsConn.WriteJSON(&WsMsg{
				Command: "command",
				Channel: channel,
				Payload: s,
			})
		},
	)

	return &WsMsg{
		Command: "sub",
		Payload: channel,
	}
}
