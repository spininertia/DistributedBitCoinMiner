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

type closeRequest struct {
	response chan struct{}
}

func newCloseRequest() *closeRequest {
	return &closeRequest{
		response: make(chan struct{}),
	}
}
