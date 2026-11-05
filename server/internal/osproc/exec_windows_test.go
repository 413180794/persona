package osproc

import (
	"os"
	"os/exec"
	"os/signal"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Exec on Windows runs the program as a child on this console and exits with its code: run this test binary as the
// launcher, with a child that exits 7.
func TestExecExitCode(t *testing.T) {
	if os.Getenv("OSPROC_EXEC_CHILD") == "1" {
		os.Exit(7)
	}
	if os.Getenv("OSPROC_EXEC_PARENT") == "1" {
		self, _ := os.Executable()
		err := Exec(self, []string{self, "-test.run=TestExecExitCode"}, append(os.Environ(), "OSPROC_EXEC_PARENT=", "OSPROC_EXEC_CHILD=1"))
		t.Fatalf("Exec returned: %v", err)
	}
	self, _ := os.Executable()
	cmd := exec.Command(self, "-test.run=TestExecExitCode")
	cmd.Env = append(os.Environ(), "OSPROC_EXEC_PARENT=1")
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 7 {
		t.Fatalf("launcher exited with %v, want code 7", err)
	}
}

// Ctrl+C on the console goes to the launcher and the child alike: the launcher stays until the child ends, so its exit
// code is still the child's. Run in a process group of its own (so the test runner is not hit) with a child that
// outlives the Ctrl+C by a moment and exits 5.
func TestExecSurvivesCtrlC(t *testing.T) {
	switch os.Getenv("OSPROC_CTRLC") {
	case "child":
		signal.Notify(make(chan os.Signal, 1), os.Interrupt) // a program that handles Ctrl+C, like claude
		time.Sleep(1500 * time.Millisecond)
		os.Exit(5)
	case "parent":
		self, _ := os.Executable()
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(os.Getpid()))
		}()
		err := Exec(self, []string{self, "-test.run=TestExecSurvivesCtrlC"}, append(os.Environ(), "OSPROC_CTRLC=child"))
		t.Fatalf("Exec returned: %v", err)
	}
	self, _ := os.Executable()
	cmd := exec.Command(self, "-test.run=TestExecSurvivesCtrlC")
	cmd.Env = append(os.Environ(), "OSPROC_CTRLC=parent")
	cmd.SysProcAttr = GroupAttr()
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 5 {
		t.Fatalf("launcher exited with %v, want the child's code 5", err)
	}
}
