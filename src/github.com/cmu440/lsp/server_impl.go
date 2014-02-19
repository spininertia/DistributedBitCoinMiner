// Contains the implementation of a LSP server.

package lsp

import (
	"container/list"
	"encoding/json"
	"errors"
	"github.com/cmu440/lspnet"
	"strconv"
	"time"
)

type server struct {
	clients       map[int]*clientHandle // map from connId to client hadnle
	conn          *lspnet.UDPConn       // udp connection
	idCounter     int                   // assign this to new client
	clientToClose map[int]*clientHandle // only initialized when Close is called

	// request queues
	readRequestQueue  *list.List // read requests queue
	closeRequestQueue *list.List // close requests queue

	// channels
	readReqChan      chan *serverReadRequest  // communicate with Read
	writeReqChan     chan *serverWriteRequest // communicate with Write
	closeConnReqChan chan *closeConnRequest   // communicate with CloseConn
	closeReqChan     chan *closeRequest       // communicate with Close
	msgArriveChan    chan *packet             // notify new msg arrives
	epochChan        chan struct{}            // receive epoch event
	shutdownNtwk     chan struct{}            // shutdown ntwk realted handler
	shutdownAll      chan struct{}            // shutdown epoch/master handler

	// params
	params *Params
}

// NewServer creates, initiates, and returns a new server. This function should
// NOT block. Instead, it should spawn one or more goroutines (to handle things
// like accepting incoming client connections, triggering epoch events at
// fixed intervals, synchronizing events using a for-select loop like you saw in
// project 0, etc.) and immediately return. It should return a non-nil error if
// there was an error resolving or listening on the specified port number.
func NewServer(port int, params *Params) (Server, error) {
	sv := &server{
		clients:           make(map[int]*clientHandle),
		idCounter:         1,
		readRequestQueue:  list.New(),
		closeRequestQueue: list.New(),
		readReqChan:       make(chan *serverReadRequest),
		writeReqChan:      make(chan *serverWriteRequest),
		closeConnReqChan:  make(chan *closeConnRequest),
		closeReqChan:      make(chan *closeRequest),
		msgArriveChan:     make(chan *packet),
		epochChan:         make(chan struct{}),
		shutdownNtwk:      make(chan struct{}),
		shutdownAll:       make(chan struct{}),
		params:            params,
	}

	// first resolve addr and listen on port
	portNum := strconv.Itoa(port)
	addr, err := lspnet.ResolveUDPAddr("udp", "localhost:"+portNum)
	if err != nil {
		return nil, err
	}

	conn, err := lspnet.ListenUDP("udp", addr)
	if err != nil {
		LOGE.Println(err)
		return nil, err
	}

	sv.conn = conn

	// start goroutines
	go sv.masterEventHandler()
	go sv.networkEventHandler()
	go sv.epochHandler()

	return sv, nil

}

func (s *server) Read() (int, []byte, error) {
	request := newServerReadRequest()
	s.readReqChan <- request
	response, ok := <-request.response
	if !ok {
		return -1, nil, errors.New("Read failed")
	}
	return response.connId, response.payload, nil
}

func (s *server) Write(connID int, payload []byte) error {
	request := newServerWriteRequest(connID, payload)
	s.writeReqChan <- request
	_, ok := <-request.response
	if !ok {
		return errors.New("The specified client does not exist")
	}
	return nil
}

func (s *server) CloseConn(connID int) error {
	request := newCloseConnRequest(connID)
	s.closeConnReqChan <- request
	_, ok := <-request.response
	if !ok {
		return errors.New("The specifed client does not exist")
	}
	return nil
}

func (s *server) Close() error {
	LOGV.Println("Server Close get called")
	request := newCloseRequest()
	s.closeReqChan <- request
	<-request.response
	return nil
}

/*
 * goroutines/ handlers
 */

// network event handler for client connId
func (s *server) networkEventHandler() {

	var buffer [Bufsize]byte
	for {
		select {
		case <-s.shutdownNtwk:
			LOGV.Println("Network Event Handler shutdown")
			return
		default:
			msg := new(Message)
			n, addr, err := s.conn.ReadFromUDP(buffer[0:])
			if err != nil {
				LOGE.Println(err)
			}
			json.Unmarshal(buffer[0:n], msg)
			s.msgArriveChan <- newPacket(msg, addr)
		}
	}
}

func (s *server) epochHandler() {
	LOGV.Println("Epoch Handler started!")

	for {
		select {
		case <-time.After(time.Duration(s.params.EpochMillis) *
			time.Millisecond):
			s.epochChan <- struct{}{}
		case <-s.shutdownAll:
			LOGV.Println("Epoch Handler shutdown!")
			return
		}
	}
}

func (s *server) masterEventHandler() {
	LOGV.Println("Master Event Handler Started!")
	defer s.conn.Close()

	for {
		select {
		case request := <-s.readReqChan:
			s.handleReadRequest(request)
		case request := <-s.writeReqChan:
			s.handleWriteRequest(request)
		case request := <-s.closeConnReqChan:
			s.handleCloseConnRequest(request)
		case request := <-s.closeReqChan:
			s.handleCloseRequest(request)
		case packet := <-s.msgArriveChan:
			s.handleNewMsg(packet.msg, packet.addr)
		case <-s.epochChan:
			s.handleEpochEvent()
		case <-s.shutdownAll:
			LOGV.Println("Master Event Handler Shutdown!")
			return
		}
	}
}

/*
 *	helper functions
 */

func (s *server) handleReadRequest(request *serverReadRequest) {
	for _, c := range s.clients {
		// lost or closed, already notified Read
		if c.isNotified {
			continue
		} else if c.isClosed {
			c.isNotified = true
			close(request.response)
			return
		} else if c.isLost && len(c.receivedMsgBuf) == 0 {
			c.isNotified = true
			close(request.response)
			// TODO: might also remove the clientHandle
			return
		} else {
			// check whether there is message satisfy the current need
			if msg, present := c.receivedMsgBuf[c.expectedSeqId]; present {
				c.expectedSeqId++
				response := newServerReadResponse(msg.ConnID, msg.Payload)
				request.response <- response
				if msg.SeqNum <= c.maxReceivedSeqId-s.params.WindowSize {
					delete(c.receivedMsgBuf, msg.SeqNum)
				}
				return
			}
		}
	}

	// if no msg satisfy the requirenment, push request into queue
	// the response to Read is pended, waiting to be handled in newMsgHandler
	s.readRequestQueue.PushBack(request)
}

func (s *server) handleWriteRequest(request *serverWriteRequest) {
	c, present := s.clients[request.connId]
	if !present || c.isLost {
		close(request.response)
	} else {
		// notify Write request accepted
		request.response <- struct{}{}

		dataMsg := NewData(request.connId, c.nextSendSeqId, request.payload)
		c.nextSendSeqId++

		// immediately send only if its within the sliding window
		if c.sentMsgBuf.Len() == 0 ||
			c.sentMsgBuf.Front().Value.(*Message).SeqNum+s.params.WindowSize >
				dataMsg.SeqNum {
			s.sendMessage(request.connId, dataMsg)
			LOGV.Println("Server Send Message:", dataMsg)
		}

		// add to sent msg buf for later resend
		c.sentMsgBuf.PushBack(dataMsg)
	}

}

func (s *server) handleCloseConnRequest(request *closeConnRequest) {
	c, present := s.clients[request.connId]
	if !present || c.isClosed {
		close(request.response)
	} else {
		c.isClosed = true
		request.response <- struct{}{}
	}
}

func (s *server) handleCloseRequest(request *closeRequest) {
	LOGV.Println("handlign close request")
	s.clientToClose = make(map[int]*clientHandle)
	for id, c := range s.clients {
		c.isClosed = true
		// check whether there is pending message to send
		if !c.isLost && c.sentMsgBuf.Len() > 0 {
			s.clientToClose[id] = c
		}
	}
	// if all client has no pending messages to send
	if len(s.clientToClose) == 0 {
		close(s.shutdownAll)
		close(s.shutdownNtwk)
		request.response <- struct{}{}
	}

	// if there is still client to close, push the close request to queue
	// pending to be processed when all msg is sent and acked
	// also when one or more connection is lost, respond with error
	s.closeRequestQueue.PushBack(request)
}

func (s *server) handleNewMsg(msg *Message, addr *lspnet.UDPAddr) {
	LOGV.Println("Server Received Message:", msg)

	// handle connect msg
	if msg.Type == MsgConnect {
		// check whether this connection is already been added
		exist := false
		for _, c := range s.clients {
			if c.addr.String() == addr.String() {
				//是这样判断addr相等吗...
				//可以之后再改一个map用用
				exist = true
				break
			}
		}

		// if not connected, establish new connection to this client
		// ack this connection
		if !exist {
			c := newClientHandle(s.idCounter, addr)
			s.idCounter++
			s.clients[c.connId] = c
			s.sendMessage(c.connId, NewAck(c.connId, 0))
			LOGV.Println("Server Acked Connect")
		}
	} else if msg.Type == MsgAck {
		c := s.clients[msg.ConnID]
		c.recvMsgLastEpoch = true

		// if lost, ignore all incomming messages
		if c.isLost {
			return
		}

		oldWindow := -1
		flag := false
		for e := c.sentMsgBuf.Front(); e != nil; e = e.Next() {
			if msg.SeqNum == e.Value.(*Message).SeqNum {
				if e == c.sentMsgBuf.Front() {
					oldWindow = msg.SeqNum + s.params.WindowSize
				}
				c.sentMsgBuf.Remove(e)
				flag = true
				break
			}
		}
		//send pending message, not waiting epoch
		if oldWindow > 0 && c.sentMsgBuf.Len() > 0 {
			newWindow := c.sentMsgBuf.Front().Value.(*Message).SeqNum +
				s.params.WindowSize
			for e := c.sentMsgBuf.Front(); e != nil; e = e.Next() {
				if msg.SeqNum >= oldWindow && msg.SeqNum < newWindow {
					s.sendMessage(c.connId, e.Value.(*Message))
				}
			}
		}

		// possibly notify close
		if flag && s.isCloseCalled() {
			if _, present := s.clientToClose[c.connId]; present {
				if c.sentMsgBuf.Len() == 0 {
					delete(s.clientToClose, c.connId)

					// all pending message of all active clients has been sent
					if len(s.clientToClose) == 0 {
						request :=
							s.closeRequestQueue.Front().Value.(*closeRequest)
						request.response <- struct{}{}
					}
				}
			}
		}
	} else if msg.Type == MsgData {
		c := s.clients[msg.ConnID]
		c.recvMsgLastEpoch = true

		// ignore lost or closed client
		if c.isLost || c.isClosed {
			return
		}

		// first check duplication
		if _, present := c.receivedMsgBuf[msg.SeqNum]; !present &&
			msg.SeqNum >= c.expectedSeqId {

			// ack this message
			s.sendMessage(c.connId, NewAck(c.connId, msg.SeqNum))
			LOGV.Println("Server Ack New Message")

			// add msg to buf
			c.receivedMsgBuf[msg.SeqNum] = msg

			// update client status
			if msg.SeqNum > c.maxReceivedSeqId {
				// remove some entries in map
				for id := c.maxReceivedSeqId - s.params.WindowSize + 1; id <=
					msg.SeqNum-s.params.WindowSize &&
					id < c.expectedSeqId; id++ {
					if _, present := c.receivedMsgBuf[id]; present {
						delete(c.receivedMsgBuf, id)
					}
				}
				c.maxReceivedSeqId = msg.SeqNum
			}

			// possibly notify read
			if s.hasPendingReadRequest() {
				if c.expectedSeqId == msg.SeqNum {
					request :=
						s.readRequestQueue.Remove(s.readRequestQueue.Front())
					request.(*serverReadRequest).response <- newServerReadResponse(c.connId,
						msg.Payload)
					c.expectedSeqId++
				}
			}

		}

	}
}

func (s *server) handleEpochEvent() {
	for _, c := range s.clients {
		// TODO: check lost first?
		if !c.isLost {
			// check timeout
			if !c.recvMsgLastEpoch {
				c.noMsgEpochCount++
				LOGV.Printf("It's been %d epoch that no msg was received\n",
					c.noMsgEpochCount)
			} else {
				c.noMsgEpochCount = 0
			}
			c.recvMsgLastEpoch = false

			// check lost
			if c.noMsgEpochCount >= s.params.EpochLimit {
				c.isLost = true
				if s.isCloseCalled() {
					request := s.closeRequestQueue.Front().Value.(*closeRequest)
					close(s.shutdownNtwk)
					close(s.shutdownAll)
					close(request.response)
					return
				}
			}

			// resend issues

			// resend ack to connect request
			// TODO: shall we check close here?
			if c.expectedSeqId == 1 && len(c.receivedMsgBuf) == 0 &&
				!c.isClosed {
				s.sendMessage(c.connId, NewAck(c.connId, 0))
				LOGV.Println("Server Resend ACK for Connect:", c.connId)
			}

			// resend messages within window and also unAcked
			if c.sentMsgBuf.Len() > 0 {
				windowUpperLimit := c.sentMsgBuf.Front().Value.(*Message).SeqNum +
					s.params.WindowSize
				for e := c.sentMsgBuf.Front(); e != nil &&
					e.Value.(*Message).SeqNum < windowUpperLimit; e = e.Next() {
					s.sendMessage(c.connId, e.Value.(*Message))
					LOGV.Println("Server Resend Data", e.Value.(*Message))
				}
			}

			// resend Acks
			// TODO: check close here
			if (c.isClosed || s.isCloseCalled()) && c.sentMsgBuf.Len() == 0 {
				continue
			}

			if c.maxReceivedSeqId > 0 {
				for seqId := c.maxReceivedSeqId; seqId > 0 &&
					seqId > c.maxReceivedSeqId-s.params.WindowSize; seqId-- {
					if msg, ok := c.receivedMsgBuf[seqId]; ok {
						ack := NewAck(c.connId, msg.SeqNum)
						s.sendMessage(c.connId, ack)
						LOGV.Println("Server Resend ACK", ack)
					}
				}
			}
		}
	}
}

func (s *server) sendMessage(connId int, msg *Message) {
	c := s.clients[connId]
	packet, _ := json.Marshal(msg)
	s.conn.WriteToUDP(packet, c.addr)
}

func (s *server) isCloseCalled() bool {
	return s.closeRequestQueue.Len() > 0
}

func (s *server) hasPendingReadRequest() bool {
	return s.readRequestQueue.Len() > 0
}
