package supervisor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

const (
	MessageTypeReady        = "ready"
	MessageTypeRestart      = "restart_request"
	MessageTypeActivate     = "activate"
	MessageTypeShutdown     = "shutdown"
	MessageTypeFailed       = "failed"
	MessageTypeDisconnected = "disconnected"
)

type ControlMessage struct {
	Type           string `json:"type"`
	WorkerID       string `json:"worker_id,omitempty"`
	ExecutablePath string `json:"executable_path,omitempty"`
	SnapshotPath   string `json:"snapshot_path,omitempty"`
	WorkspaceRoot  string `json:"workspace_root,omitempty"`
	Error          string `json:"error,omitempty"`
}

type controlPeer interface {
	Send(message ControlMessage) error
}

type SwitchCoordinator struct {
	mu          sync.Mutex
	activeID    string
	active      controlPeer
	pendingID   string
	pendingPeer controlPeer
}

func NewSwitchCoordinator() *SwitchCoordinator {
	return &SwitchCoordinator{}
}

func (c *SwitchCoordinator) SetActive(workerID string, peer controlPeer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeID = workerID
	c.active = peer
}

func (c *SwitchCoordinator) ActiveWorkerID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.activeID
}

func (c *SwitchCoordinator) BeginPromotion(workerID string, peer controlPeer) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.active != nil {
		if err := c.active.Send(ControlMessage{Type: MessageTypeShutdown}); err != nil {
			return fmt.Errorf("通知旧 worker 关闭失败: %w", err)
		}
	}
	c.pendingID = workerID
	c.pendingPeer = peer
	return nil
}

func (c *SwitchCoordinator) CompletePromotion() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pendingPeer == nil {
		return fmt.Errorf("没有待激活的 standby worker")
	}
	if err := c.pendingPeer.Send(ControlMessage{Type: MessageTypeActivate}); err != nil {
		return fmt.Errorf("激活新 worker 失败: %w", err)
	}
	c.activeID = c.pendingID
	c.active = c.pendingPeer
	c.pendingID = ""
	c.pendingPeer = nil
	return nil
}

type ControlClient struct {
	workerID string
	conn     net.Conn
	encoder  *json.Encoder
	incoming chan ControlMessage
}

func DialControlClient(addr, workerID string) (*ControlClient, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	client := &ControlClient{
		workerID: workerID,
		conn:     conn,
		encoder:  json.NewEncoder(conn),
		incoming: make(chan ControlMessage, 8),
	}
	go client.readLoop()
	return client, nil
}

func (c *ControlClient) Send(message ControlMessage) error {
	if message.WorkerID == "" {
		message.WorkerID = c.workerID
	}
	return c.encoder.Encode(message)
}

func (c *ControlClient) Incoming() <-chan ControlMessage {
	return c.incoming
}

func (c *ControlClient) Close() error {
	return c.conn.Close()
}

func (c *ControlClient) readLoop() {
	defer close(c.incoming)

	scanner := bufio.NewScanner(c.conn)
	for scanner.Scan() {
		var message ControlMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			c.incoming <- ControlMessage{Type: MessageTypeFailed, Error: err.Error()}
			return
		}
		c.incoming <- message
	}
}

type workerConn struct {
	id      string
	conn    net.Conn
	encoder *json.Encoder
}

func (w *workerConn) Send(message ControlMessage) error {
	if message.WorkerID == "" {
		message.WorkerID = w.id
	}
	return w.encoder.Encode(message)
}

type WorkerEvent struct {
	WorkerID string
	Message  ControlMessage
}

type ControlServer struct {
	listener net.Listener
	events   chan WorkerEvent

	mu      sync.Mutex
	workers map[string]*workerConn
}

func NewControlServer() (*ControlServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	server := &ControlServer{
		listener: listener,
		events:   make(chan WorkerEvent, 32),
		workers:  make(map[string]*workerConn),
	}
	go server.acceptLoop()
	return server, nil
}

func (s *ControlServer) Addr() string {
	return s.listener.Addr().String()
}

func (s *ControlServer) Events() <-chan WorkerEvent {
	return s.events
}

func (s *ControlServer) Close() error {
	err := s.listener.Close()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, worker := range s.workers {
		_ = worker.conn.Close()
	}
	return err
}

func (s *ControlServer) Send(workerID string, message ControlMessage) error {
	s.mu.Lock()
	worker := s.workers[workerID]
	s.mu.Unlock()

	if worker == nil {
		return fmt.Errorf("worker %s 未连接", workerID)
	}
	return worker.Send(message)
}

func (s *ControlServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			close(s.events)
			return
		}
		go s.handleConn(conn)
	}
}

func (s *ControlServer) handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	var currentID string
	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		var message ControlMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			s.events <- WorkerEvent{
				WorkerID: currentID,
				Message: ControlMessage{
					Type:    MessageTypeFailed,
					WorkerID: currentID,
					Error:   err.Error(),
				},
			}
			return
		}
		if message.WorkerID != "" && currentID == "" {
			currentID = message.WorkerID
			s.mu.Lock()
			s.workers[currentID] = &workerConn{id: currentID, conn: conn, encoder: encoder}
			s.mu.Unlock()
		}

		s.events <- WorkerEvent{
			WorkerID: currentID,
			Message:  message,
		}
	}

	if currentID != "" {
		s.mu.Lock()
		delete(s.workers, currentID)
		s.mu.Unlock()
		s.events <- WorkerEvent{
			WorkerID: currentID,
			Message: ControlMessage{
				Type:     MessageTypeDisconnected,
				WorkerID: currentID,
			},
		}
	}
}
