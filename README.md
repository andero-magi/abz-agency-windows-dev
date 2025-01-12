# Proxy Monitor
Windows Developer test assignment, written in Golang.

The program works by repeatedly checking the `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Internet Settings` registry values. If they've changed, the change is logged.
  
When separate instances of the program are started, commands are communicated to the first instance of the program with Named Pipes.

## Used libraries
- [`github.com/Microsoft/go-winio`](https://github.com/Microsoft/go-winio)  
  Microsoft library for using Win32 IO utlities. In this project it's used to 
  interact with Named Pipes and to allow instances of the monitor program to 
  communicate with each other.
- [`github.com/allan-simon/go-singleinstance`](https://github.com/allan-simon/go-singleinstance)  
  A Go library for running only one instance of a program.
- [`golang.org/x/sys/windows/registry`](https://pkg.go.dev/golang.org/x/sys/windows/registry)  
  Provides access to the Windows Registry API.
- [`github.com/getlantern/systray`](https://github.com/getlantern/systray)  
  Cross-platform library for creating a tray icon and menu.

## Build instructions
1. Clone the repo.
   ```bat
   git clone https://github.com/andero-magi/abz-agency-windows-dev
   ```
2. CD into the project directory.
   ```bat
   cd abz-agency-windows-dev
   ```
3. Download the project dependencies
   ```bat
   go mod download
   ```
4. Build the project
   ```bat
   go build
   ```
5. Run the program
   ```bat
   proxy-monitor
   ```

## CLI Commands
- Start the program and start monitoring
  ```txt
  proxy-monitor
  ```
  OR
  ```txt
  proxy-monitor -start
  ```
- Stop monitoring
  ```txt
  proxy-monitor -stop
  ```
- Close the program
  ```txt
  proxy-monitor -quit
  ```