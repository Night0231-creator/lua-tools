package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("UPDATE_PUBLIC_KEY=" + base64.StdEncoding.EncodeToString(pub))
	fmt.Println("UPDATE_PRIVATE_KEY=" + base64.StdEncoding.EncodeToString(priv))
	fmt.Println("Guarde UPDATE_PRIVATE_KEY apenas em Secrets do GitHub. Nunca commite esta chave.")
}
