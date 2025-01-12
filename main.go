package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Named pipes library
	"github.com/Microsoft/go-winio"

	// Single instance library
	"github.com/allan-simon/go-singleinstance"

	// System tray library and their example icon
	"github.com/getlantern/systray"
	"github.com/getlantern/systray/example/icon"

	// Registry access API
	"golang.org/x/sys/windows/registry"
)

// Name of the process lock file
const LOCK_FILE = "monitor.lock"

// Name of the named pipe used to communicate between the main monitor
// process and newer instances
const PIPE_FILE = `\\.\pipe\proxymonitor`

// Command constants, used to internally represent the
// commands stop, start and quit
// These values are also sent between processes
const NO_COMMAND byte = 0
const CMD_STOP byte = 1
const CMD_QUIT byte = 2
const CMD_START byte = 3

// Global variable that controls the state of the listener
var listenerEnabled bool = true

// Parses the command line argument into one of the command constants
func parseCommand() (byte, error) {
	args := os.Args
	len := len(args)

	if len < 2 {
		return NO_COMMAND, nil
	}

	arg := os.Args[1]

	switch arg {
	case "-stop":
		return CMD_STOP, nil
	case "-start":
		return CMD_START, nil
	case "-quit":
		return CMD_QUIT, nil
	default:
		return NO_COMMAND, fmt.Errorf("unknown command: %s", arg)
	}
}

// When several instances of this process are started, the oldest one is the
// process that actually does the monitoring, it starts up in a different way,
// the only purpose of the newer instances is to communicate a command to the
// main instance
func clientMain() {
	// Connect to the named pipe
	f, err := winio.DialPipe(PIPE_FILE, nil)
	if err != nil {
		fmt.Println("Failed to dial to pipe", err)
		return
	}

	defer f.Close()

	// Parse the command line argument, which will be sent to
	// the main program instance
	parsedCmd, err := parseCommand()
	if err != nil {
		fmt.Println("Failed to parse command line arguments:", err)
		return
	}

	// Create a buffer that will be used to store the information sent between
	// this instance and the main one
	buf := make([]byte, 1)
	buf[0] = parsedCmd

	// Send the command
	_, err = f.Write(buf)

	if err != nil {
		fmt.Println("Failed to write bytes", err)
		return
	}

	// When a QUIT command is sent, the main process exits without sending a
	// response, so don't try to read it.
	if parsedCmd == CMD_QUIT {
		return
	}

	// Read the response from the main program instance, it will always be either
	// a 0 or 1, depending on if the command was carried out successfully
	_, err = f.Read(buf)
	if err != nil {
		fmt.Println("Failed to read response from main program instance:", err)
		return
	}

	// This part just takes the 0 or 1 reply, turns it into a boolean and then
	// prints a message corresponding to the command that was issued and if it
	// was successful
	success := buf[0] == 1
	var message string

	switch parsedCmd {
	case CMD_START:
		if success {
			message = "Started monitoring proxy settings."
		} else {
			message = "Already monitoring proxy settings."
		}
	case CMD_STOP:
		if success {
			message = "Stopped monitoring proxy settings"
		} else {
			message = "Proxy monitor is already turned off."
		}
	case CMD_QUIT:
		// QUIT command can never fail
		message = "Quitting monitor program..."
	default:
		return
	}

	fmt.Println(message)
}

// As stated above in the clientMain() comment, the main program instance starts
// up differently than newer instances. This is the entry point for the first
// instance of the program
func serverMain() {
	cmd, err := parseCommand()

	if err != nil {
		fmt.Println("Failed to read command line arguments", err)
	}

	// Just stop right away
	if cmd == CMD_QUIT {
		return
	}

	// Start up the named pipe and listen to commands from other
	// instances of this program
	go listenToNamedPipe()
	go createSystemTrayIcon()

	if cmd == NO_COMMAND || cmd == CMD_START {
		startListening()
	}

	listenToProxyChanges()
}

func createSystemTrayIcon() {
	systray.Run(
		func() {
			systray.SetIcon(icon.Data)
			systray.SetTitle("Proxy Monitor")

			start := systray.AddMenuItem("Start", "Start monitoring")
			stop := systray.AddMenuItem("Stop", "Stop monitoring")
			quit := systray.AddMenuItem("Quit", "Quit monitoring")

			go func() {
				for {
					select {
					case <-start.ClickedCh:
						if !listenerEnabled {
							startListening()
						}

					case <-stop.ClickedCh:
						if listenerEnabled {
							stopListening()
						}

					case <-quit.ClickedCh:
						os.Exit(0)
					}
				}
			}()
		},
		nil)
}

func openLogFile() (*os.File, error) {
	logDir := filepath.Join(os.Getenv("appdata"), "proxy-monitor")
	dirErr := os.MkdirAll(logDir, os.ModePerm)
	if dirErr != nil {
		return nil, dirErr
	}

	// File access permissions: We can read/write the file,
	// Everyone can read/write the file
	var permissions os.FileMode = 0666

	logPath := filepath.Join(logDir, "proxy-monitor.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, permissions)

	return logFile, err
}

// Detects changes in a loop in the windows registry
func listenToProxyChanges() {
	const keypath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Internet Settings`

	// Get a HANDLE for the key to monitor
	key, err := registry.OpenKey(registry.CURRENT_USER, keypath, registry.QUERY_VALUE)

	if err != nil {
		fmt.Println("Error opening registry key", err)
		return
	}

	defer key.Close()

	logFile, err := openLogFile()
	if err != nil {
		fmt.Println("Failed to open log file:", err)
		return
	}

	fmt.Println("Logging output to", logFile.Name())

	// Track the last known proxy enabled and proxy server states
	var lastProxyEnable uint64 = 0
	var lastProxyServer string = ""

	// A nested function that checks if any of the settings have changed.
	// Returns true if the program should continue checking for updates, false
	// for if the program should end.
	// Returns false in the case of errors
	var checkForChanges = func() bool {
		if !listenerEnabled {
			return true
		}

		// Read the ProxyEnable setting
		proxyEnable, _, err := key.GetIntegerValue("ProxyEnable")
		if err != nil {
			fmt.Println("Failed to read ProxyEnable:", err)
			return false
		}

		// Read the IP address of the proxy
		proxyServer, _, err := key.GetStringValue("ProxyServer")
		if err != nil {
			// ProxyEnable will always exist in the registry, but there's a
			// chance that the ProxyServer value isn't set yet
			if err == registry.ErrNotExist {
				lastProxyServer = ""
			} else {
				fmt.Println("Failed to read ProxyServer:", err)
				return false
			}
		}

		// If neither value has changed, then there's nothing to log, stop here
		if proxyEnable == lastProxyEnable && proxyServer == lastProxyServer {
			return true
		}

		lastProxyEnable = proxyEnable
		lastProxyServer = proxyServer

		// Get the time, for the log messages
		now := time.Now()
		formattedTime := now.Format(time.ANSIC)

		// Off messages shouldn't have any information after the 'off' part
		if proxyEnable == 0 {
			fmt.Fprintf(logFile, "%s\tproxy off\n", formattedTime)
			return true
		}

		fmt.Fprintf(logFile, "%s\tproxy on, %s\n", formattedTime, proxyServer)
		return true
	}

	// Check for changes every second, unless the check function returns false
	for {
		if !checkForChanges() {
			return
		}

		time.Sleep(1 * time.Second)
	}
}

// Enable the monitor
func startListening() {
	listenerEnabled = true
	fmt.Println("Now listening to proxy changes")
}

// Disable the monitor
func stopListening() {
	listenerEnabled = false
	fmt.Println("No longer listening to proxy changes")
}

// Listens to messages from other instances of this program
func listenToNamedPipe() {
	// Listen to pipe messages
	l, err := winio.ListenPipe(PIPE_FILE, nil)
	if err != nil {
		fmt.Println("Failed to listen to pipe!", err)
		return
	}

	defer l.Close()

	// We only read 1 byte (the command number) and send 1 byte (a 0 or 1),
	// so allocate a 1 byte long buffer
	buffer := make([]byte, 1)

	for {
		conn, err := l.Accept()

		if err != nil {
			fmt.Println("Failed to read pipe input", err)
			continue
		}

		_, err = conn.Read(buffer)
		if err != nil {
			fmt.Println("Failed to read", err)
			conn.Close()
			continue
		}

		// Get the command that was read and execute it
		cmd := buffer[0]
		execRes := executeCommand(cmd)

		if execRes {
			buffer[0] = 1
		} else {
			buffer[0] = 0
		}

		// Send the 0 or 1 back to the process to let it know if the
		// command was successful or not
		_, err = conn.Write(buffer)
		conn.Close()

		if err == nil {
			continue
		}

		fmt.Println("Failed to write pipe response:", err)
	}
}

// Executes a command sent from another instance of this program
func executeCommand(cmd byte) bool {
	switch cmd {
	case CMD_START:
		if listenerEnabled {
			return false
		}
		startListening()

	case CMD_STOP:
		if !listenerEnabled {
			return false
		}
		stopListening()

	case CMD_QUIT:
		fmt.Println("Exiting...")
		os.Exit(0)
	}

	return true
}

func main() {
	// Get the lock file
	_, err := singleinstance.CreateLockFile(LOCK_FILE)

	// Error will not be nil when another process is using the lock file.
	// That means there's already an instance of this program running.
	if err != nil {
		clientMain()
		return
	}

	// Lock file doesn't exist or references a process that no longer exists,
	// this process is now the main instance of this program
	serverMain()
}
