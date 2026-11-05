//go:build !unix

package hub

func closeOnExec(int) {}

func keepOnExec(int) {}
