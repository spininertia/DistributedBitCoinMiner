package main

import (
	"encoding/json"
	"fmt"
	"github.com/cmu440/bitcoin"
	"github.com/cmu440/lsp"
	"os"
)

func main() {
	const numArgs = 4
	if len(os.Args) != numArgs {
		fmt.Println("Usage: ./client <hostport> <message> <maxNonce>")
		return
	}

	hostport := os.Args[1]
	message := os.Args[2]
	maxNonce := uint64(os.Args[3])

	client := lsp.NewClient(os.Args[1], lsp.NewParams())
	defer client.Close()

	// write request
	payload, _ := json.Marshal(bitcoin.NewRequest(message, 0, maxNonce))
	client.Write(payload)

	// read response
	response, err := client.Read()
	if err != nil {
		printDisconnected()
	} else {
		result := new(bitcoin.Message)
		json.Unmarshal(response, result)
		printResult(result.Hash, result.Nonce)
	}
}

// printResult prints the final result to stdout.
func printResult(hash, nonce string) {
	fmt.Println("Result", hash, nonce)
}

// printDisconnected prints a disconnected message to stdout.
func printDisconnected() {
	fmt.Println("Disconnected")
}
