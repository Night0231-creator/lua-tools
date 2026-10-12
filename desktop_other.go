//go:build !windows

package main

import "fmt"

func runDesktop(a *App) {
	fmt.Println("A interface nativa do Astronyx Steam Manager funciona no Windows 10/11.")
}
func showError(msg string) { fmt.Println(msg) }
