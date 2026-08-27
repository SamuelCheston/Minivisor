package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

type ScreenManager struct {
	SocketDir string
	LogDir    string
	mu        sync.Mutex
	// 记录每个会话的 "master attach" 进程，用于向所有订阅者分发数据
	attaches map[string]*screenAttach
}

type screenAttach struct {
	cmd  *exec.Cmd
	pty  *os.File
	subs map[chan []byte]struct{}
	mu   sync.Mutex
}

func NewScreenManager(baseDir string) (*ScreenManager, error) {
	socketDir := filepath.Join(baseDir, "sockets")
	logDir := filepath.Join(baseDir, "logs")

	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	return &ScreenManager{
		SocketDir: socketDir,
		LogDir:    logDir,
		attaches:  make(map[string]*screenAttach),
	}, nil
}

func (sm *ScreenManager) screenCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("screen", args...)
	cmd.Env = append(os.Environ(), "SCREENDIR="+sm.SocketDir)
	return cmd
}

func (sm *ScreenManager) Start(id, workDir, command string) error {
	logFile := filepath.Join(sm.LogDir, id+".log")
	// -dmS: 启动一个分离的会话
	// -L -Logfile: 开启日志记录
	args := []string{"-dmS", id, "-L", "-Logfile", logFile, "bash", "-c", command}
	cmd := sm.screenCmd(args...)
	cmd.Dir = workDir
	return cmd.Run()
}

func (sm *ScreenManager) Stop(id string) error {
	// 使用 quit 命令停止 screen 会话
	return sm.screenCmd("-S", id, "-X", "quit").Run()
}

func (sm *ScreenManager) Kill(id string) error {
	// 尝试先优雅停止，如果不行则通过 pid kill
	pid, err := sm.GetPID(id)
	if err != nil {
		return err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func (sm *ScreenManager) SendInput(id string, data []byte) error {
	// 使用 stuff 命令发送输入
	return sm.screenCmd("-S", id, "-X", "stuff", string(data)).Run()
}

func (sm *ScreenManager) Resize(id string, cols, rows int) error {
	// screen 不支持直接通过命令行 resize 远程会话，但我们可以通过附加的 PTY 来影响它
	// 或者发送命令给会话内部
	resizeCmd := fmt.Sprintf("width %d %d\n", cols, rows)
	return sm.screenCmd("-S", id, "-X", "eval", resizeCmd).Run()
}

func (sm *ScreenManager) IsRunning(id string) (bool, error) {
	output, err := sm.screenCmd("-ls").Output()
	if err != nil {
		// screen -ls 返回非0通常表示没有正在运行的会话
		return false, nil
	}
	return strings.Contains(string(output), "."+id+"\t"), nil
}

func (sm *ScreenManager) GetPID(id string) (int, error) {
	output, err := sm.screenCmd("-ls").Output()
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "."+id+"\t") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				sessionInfo := parts[0] // e.g., 1234.script-id
				pidStr := strings.Split(sessionInfo, ".")[0]
				var pid int
				fmt.Sscanf(pidStr, "%d", &pid)
				return pid, nil
			}
		}
	}
	return 0, fmt.Errorf("session not found")
}

func (sm *ScreenManager) ListRunning() ([]string, error) {
	output, err := sm.screenCmd("-ls").Output()
	if err != nil {
		return nil, nil
	}
	var ids []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "There is") || strings.HasPrefix(line, "No Sockets") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			sessionInfo := parts[0]
			idParts := strings.Split(sessionInfo, ".")
			if len(idParts) > 1 {
				ids = append(ids, idParts[1])
			}
		}
	}
	return ids, nil
}

// Attach 返回一个用于接收输出的 channel，并处理输入转发
func (sm *ScreenManager) Attach(id string, sub chan []byte) error {
	sm.mu.Lock()
	attach, ok := sm.attaches[id]
	if !ok {
		// 创建新的 master attach
		cmd := sm.screenCmd("-x", id)
		f, err := pty.Start(cmd)
		if err != nil {
			sm.mu.Unlock()
			return err
		}

		attach = &screenAttach{
			cmd:  cmd,
			pty:  f,
			subs: make(map[chan []byte]struct{}),
		}
		sm.attaches[id] = attach

		go sm.consumeAttach(id, attach)
	}
	sm.mu.Unlock()

	attach.mu.Lock()
	attach.subs[sub] = struct{}{}
	attach.mu.Unlock()

	return nil
}

func (sm *ScreenManager) Detach(id string, sub chan []byte) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	attach, ok := sm.attaches[id]
	if !ok {
		return
	}

	attach.mu.Lock()
	delete(attach.subs, sub)
	count := len(attach.subs)
	attach.mu.Unlock()

	if count == 0 {
		// 没有订阅者了，关闭 master attach 进程
		attach.pty.Close()
		if attach.cmd.Process != nil {
			attach.cmd.Process.Signal(syscall.SIGTERM)
		}
		delete(sm.attaches, id)
	}
}

func (sm *ScreenManager) consumeAttach(id string, attach *screenAttach) {
	defer attach.pty.Close()

	buf := make([]byte, 4096)
	for {
		n, err := attach.pty.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			attach.mu.Lock()
			for sub := range attach.subs {
				select {
				case sub <- data:
				default:
				}
			}
			attach.mu.Unlock()
		}
		if err != nil {
			break
		}
	}

	sm.mu.Lock()
	if sm.attaches[id] == attach {
		delete(sm.attaches, id)
	}
	sm.mu.Unlock()
}

func (sm *ScreenManager) GetLogs(id string) ([]string, error) {
	logFile := filepath.Join(sm.LogDir, id+".log")
	f, err := os.Open(logFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
