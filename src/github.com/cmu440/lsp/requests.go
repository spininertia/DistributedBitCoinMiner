// define read and close request types

package lsp

type readRequest struct {
	response chan *Message
}

func newReadRequest() *readRequest {
	return &readRequest{
		response: make(chan *Message),
	}
}

type writeRequest struct {
	payload  []byte
	response chan struct{}
}

func newWriteRequest(payload []byte) *writeRequest {
	return &writeRequest{
		payload:  pyload,
		response: make(chan struct{}),
	}
}

type closeRequest struct {
	response chan struct{}
}

func newCloseRequest() *closeRequest {
	return &closeRequest{
		response: make(chan struct{}),
	}
}
