package main

import (
	"bufio"
	"code.google.com/p/go.net/websocket"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
)

const (
	chatPort = 4001
	msgBuf   = 16
	maxMsg   = 1024
)

var config struct {
	User string
	Port string
	Key  []byte
}

var (
	Incoming = make(chan []byte, msgBuf)
	Outgoing = make(chan []byte, msgBuf)
)

func transmitterHandler(ws *websocket.Conn) {
	buf := bufio.NewReader(ws)
	for {
		msg, err := buf.ReadBytes('\n')
		if err == io.EOF {
			log.Println("lost socket")
			return
		} else if err != nil {
			log.Println("error reading from websocket: ", err.Error())
			continue
		}
		Incoming <- msg
	}
}

func receiverHandler(ws *websocket.Conn) {
	messages := make([][]byte, 0)
	msgCount := len(Outgoing)
	if msgCount == 0 {
		return
	}
	for i := 0; i < msgCount; i++ {
		messages = append(messages, <-Outgoing)
	}

	wire, err := json.Marshal(messages)
	if err != nil {
		ws.Close()
	}
	ws.Write(wire)
}

func main() {
	fKeyFile := flag.String("k", "", "key file")
	fPort := flag.Int("p", 4000, "listening port")
	fUser := flag.String("u", "anonymous", "user to broadcast as")
	flag.Parse()

	config.Port = fmt.Sprintf("%d", *fPort)
	config.User = *fUser

	if *fKeyFile != "" {
		var err error
		config.Key, err = ReadKeyFromFile(*fKeyFile)
		if err != nil {
			log.Fatalf("[!] failed to load %s: %s\n", *fKeyFile,
				err.Error())
		}
	}

	go networkChat()
	http.HandleFunc("/", rootHandler)
	http.Handle("/socket", websocket.Handler(transmitterHandler))
	http.Handle("/incoming", websocket.Handler(receiverHandler))
	log.Fatal(http.ListenAndServe(":"+config.Port, nil))
}

func networkChat() {
	gaddr, ifi := selectInterface()
	log.Println("listening on ", ifi.Name)
        log.Println("using multicast address ", gaddr.String())
	go transmit(gaddr)
	go receive(gaddr, ifi)
}

func transmit(gaddr *net.UDPAddr) {
	for {
		msg, ok := <-Incoming
		if !ok {
			log.Println("transmit channel closed")
			return
		}
		broadcast, err := EncodeMessage(msg)
		if err != nil {
			log.Println("failed to encode message: ", err.Error())
			continue
		}
		uc, err := net.DialUDP("udp", nil, gaddr)
		if err != nil {
			log.Println("failed to dial multicast: ", err.Error())
			continue
		}
		var n int
		n, err = uc.Write(broadcast)
		if err != nil {
			log.Println("failed to send message: ", err.Error())
			continue
		} else if n != len(broadcast) {
			log.Printf("warning: short message sent (%d / %d bytes)",
				n, len(broadcast))
		}
	}
}

func receive(gaddr *net.UDPAddr, ifi *net.Interface) {
	for {
		uc, err := net.ListenMulticastUDP("udp", ifi, gaddr)
		if err != nil {
			log.Fatal("failed to set up multicast listener: ",
				err.Error())
		}
		msg := make([]byte, maxMsg)
		n, _, err := uc.ReadFrom(msg)
		if err != nil {
			log.Println("error reading incoming message: ", err.Error())
			continue
		} else if n == 0 {
			continue
		}
		out, err := DecodeMessage(msg[:n])
		if err != nil {
			log.Println("failed to decode message: ", err.Error())
			log.Println("msg: %s\n\t%+v", string(msg), msg)
			continue
		}
		Outgoing <- []byte(out)
	}
}
