package main

import (
	"encoding/json"
	"fmt"
	"github.com/cmu440/bitcoin"
	"github.com/cmu440/lsp"
	"os"
	"strconv"
)

func main() {
	const numArgs = 4
	if len(os.Args) != numArgs {
		fmt.Println("Usage: ./client <hostport> <message> <maxNonce>")
		return
	}

	hostport := os.Args[1]
	message := os.Args[2]
	maxNonce, _ := strconv.ParseUint(os.Args[3], 10, 64)
	client, _ := lsp.NewClient(hostport, lsp.NewParams())
	defer client.Close()

	// write request
	payload, _ := json.Marshal(bitcoin.NewRequest(message, 0, maxNonce))

	if err := client.Write(payload); err != nil {
		printDisconnected()
		return
	}

	// read response
	response, err := client.Read()
	if err != nil {
		printDisconnected()
	} else {
		result := new(bitcoin.Message)
		json.Unmarshal(response, result)
		printResult(strconv.FormatUint(result.Hash, 10),
			strconv.FormatUint(result.Nonce, 10))
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
