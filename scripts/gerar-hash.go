package main

import (
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"os"
)

func main() {
	senha := "changeme"
	if len(os.Args) > 1 {
		senha = os.Args[1]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}

