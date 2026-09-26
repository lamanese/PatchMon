package agentregistry

import (
	"testing"

	"github.com/gorilla/websocket"
)

// A stale teardown of an old connection must not evict the connection a
// reconnect has already installed for the same api_id.
func TestUnregisterConn_StaleTeardownKeepsNewConnection(t *testing.T) {
	r := New()
	oldConn := &websocket.Conn{}
	newConn := &websocket.Conn{}

	r.Register("agent-1", true)
	r.SetConnection("agent-1", oldConn)

	// Agent reconnects before the old connection's teardown runs.
	r.Register("agent-1", true)
	r.SetConnection("agent-1", newConn)

	if owned := r.UnregisterConn("agent-1", oldConn); owned {
		t.Fatal("stale teardown must not own the slot of a newer connection")
	}
	if !r.IsConnected("agent-1") {
		t.Fatal("agent must stay connected after a stale teardown")
	}
	if !r.Get("agent-1").Connected {
		t.Fatal("meta must stay connected after a stale teardown")
	}
	if e := r.getEntry("agent-1"); e == nil || e.ws != newConn {
		t.Fatal("new connection must remain registered")
	}
}

func TestUnregisterConn_CurrentConnectionIsRemoved(t *testing.T) {
	r := New()
	conn := &websocket.Conn{}

	r.Register("agent-1", false)
	r.SetConnection("agent-1", conn)

	if owned := r.UnregisterConn("agent-1", conn); !owned {
		t.Fatal("teardown of the current connection must own the slot")
	}
	if r.IsConnected("agent-1") {
		t.Fatal("agent must be disconnected after its own teardown")
	}
}
