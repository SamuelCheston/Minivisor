package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed dist/*
var frontendContent embed.FS

const (
	configFileName      = "config.json"
	daemonRootDirName   = "daemons"
	scriptsStoreName    = "scripts.json"
	scriptFilesDirName  = "scripts"
	maxBufferedLogLines = 500
)

type Config struct {
	Port int    `json:"port"`
	Name string `json:"name"`
}

type Script struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	WorkDir   string    `json:"workDir"`
	Content   string    `json:"content"`
	AutoStart bool      `json:"autoStart"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type scriptStore struct {
	Scripts []Script `json:"scripts"`
}

type ScriptPayload struct {
	Name      string `json:"name"`
	WorkDir   string `json:"workDir"`
	Content   string `json:"content"`
	AutoStart bool   `json:"autoStart"`
}

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
}

type ScriptView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	WorkDir   string     `json:"workDir"`
	Content   string     `json:"content"`
	AutoStart bool       `json:"autoStart"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Status    string     `json:"status"`
	PID       int        `json:"pid,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
}

type ManagedScript struct {
	Config      Script
	Status      string
	PID         int
	StartedAt   *time.Time
	Logs        []LogEntry
	Cmd         *exec.Cmd
	Subscribers map[chan LogEntry]struct{}
}

type App struct {
	config      Config
	configPath  string
	storePath   string
	scriptFiles string

	mu      sync.RWMutex
	scripts map[string]*ManagedScript
}

func setupEnvironment() (Config, string, string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return Config{}, "", "", err
	}

	// 查找配置文件，先在当前目录找，找不到去上级目录找
	configPath := filepath.Join(wd, configFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		parentConfig := filepath.Join(wd, "..", configFileName)
		if _, err := os.Stat(parentConfig); err == nil {
			configPath = parentConfig
		}
	}

	// 确定数据目录 (daemons) 应该与配置文件在同一级
	daemonRoot := filepath.Join(filepath.Dir(configPath), daemonRootDirName)
	storePath := filepath.Join(daemonRoot, scriptsStoreName)
	scriptFiles := filepath.Join(daemonRoot, scriptFilesDirName)

	if mkdirErr := os.MkdirAll(scriptFiles, 0755); mkdirErr != nil {
		return Config{}, "", "", mkdirErr
	}

	if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
		defaultConfig := Config{
			Port: 7891,
			Name: "Tinyvisor Service",
		}
		data, marshalErr := json.MarshalIndent(defaultConfig, "", "  ")
		if marshalErr != nil {
			return Config{}, "", "", marshalErr
		}
		if writeErr := os.WriteFile(configPath, data, 0644); writeErr != nil {
			return Config{}, "", "", writeErr
		}
	}

	if _, statErr := os.Stat(storePath); os.IsNotExist(statErr) {
		emptyStore := scriptStore{Scripts: []Script{}}
		data, marshalErr := json.MarshalIndent(emptyStore, "", "  ")
		if marshalErr != nil {
			return Config{}, "", "", marshalErr
		}
		if writeErr := os.WriteFile(storePath, data, 0644); writeErr != nil {
			return Config{}, "", "", writeErr
		}
	}

	var config Config
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, "", "", err
	}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return Config{}, "", "", err
	}
	if config.Port == 0 {
		config.Port = 8080
	}
	if strings.TrimSpace(config.Name) == "" {
		config.Name = "Tinyvisor Service"
	}

	return config, storePath, scriptFiles, nil
}

func newApp(config Config, storePath, scriptFiles string) (*App, error) {
	app := &App{
		config:      config,
		configPath:  filepath.Join(filepath.Dir(storePath), "..", configFileName),
		storePath:   storePath,
		scriptFiles: scriptFiles,
		scripts:     make(map[string]*ManagedScript),
	}

	if err := app.loadScripts(); err != nil {
		return nil, err
	}

	return app, nil
}

func (a *App) loadScripts() error {
	data, err := os.ReadFile(a.storePath)
	if err != nil {
		return err
	}

	var store scriptStore
	if len(data) > 0 {
		if err := json.Unmarshal(data, &store); err != nil {
			return err
		}
	}

	for _, storedScript := range store.Scripts {
		a.scripts[storedScript.ID] = &ManagedScript{
			Config:      storedScript,
			Status:      "stopped",
			Subscribers: make(map[chan LogEntry]struct{}),
			Logs:        []LogEntry{},
		}
	}

	return nil
}

func (a *App) saveScripts() error {
	a.mu.RLock()
	scripts := make([]Script, 0, len(a.scripts))
	for _, item := range a.scripts {
		scripts = append(scripts, item.Config)
	}
	a.mu.RUnlock()

	sort.Slice(scripts, func(i, j int) bool {
		return scripts[i].CreatedAt.Before(scripts[j].CreatedAt)
	})

	data, err := json.MarshalIndent(scriptStore{Scripts: scripts}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(a.storePath, data, 0644)
}

func (a *App) listScripts() []ScriptView {
	a.mu.RLock()
	defer a.mu.RUnlock()

	items := make([]ScriptView, 0, len(a.scripts))
	for _, item := range a.scripts {
		items = append(items, toScriptView(item))
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})

	return items
}

func toScriptView(item *ManagedScript) ScriptView {
	return ScriptView{
		ID:        item.Config.ID,
		Name:      item.Config.Name,
		WorkDir:   item.Config.WorkDir,
		Content:   item.Config.Content,
		AutoStart: item.Config.AutoStart,
		CreatedAt: item.Config.CreatedAt,
		UpdatedAt: item.Config.UpdatedAt,
		Status:    item.Status,
		PID:       item.PID,
		StartedAt: item.StartedAt,
	}
}

func validatePayload(payload ScriptPayload) (ScriptPayload, error) {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.WorkDir = strings.TrimSpace(payload.WorkDir)
	payload.Content = strings.TrimSpace(payload.Content)

	if payload.Name == "" {
		return payload, errors.New("脚本名称不能为空")
	}
	if payload.WorkDir == "" {
		return payload, errors.New("运行目录不能为空")
	}

	workDir, err := filepath.Abs(payload.WorkDir)
	if err != nil {
		return payload, errors.New("运行目录格式无效")
	}

	info, err := os.Stat(workDir)
	if err != nil || !info.IsDir() {
		return payload, errors.New("运行目录不存在或不是目录")
	}

	if payload.Content == "" {
		return payload, errors.New("脚本内容不能为空")
	}

	payload.WorkDir = workDir
	return payload, nil
}

func generateID() string {
	return fmt.Sprintf("script-%d", time.Now().UnixNano())
}

func (a *App) createScript(payload ScriptPayload) (ScriptView, error) {
	validated, err := validatePayload(payload)
	if err != nil {
		return ScriptView{}, err
	}

	now := time.Now()
	script := Script{
		ID:        generateID(),
		Name:      validated.Name,
		WorkDir:   validated.WorkDir,
		Content:   validated.Content,
		AutoStart: validated.AutoStart,
		CreatedAt: now,
		UpdatedAt: now,
	}

	item := &ManagedScript{
		Config:      script,
		Status:      "stopped",
		Subscribers: make(map[chan LogEntry]struct{}),
		Logs:        []LogEntry{},
	}

	a.mu.Lock()
	a.scripts[script.ID] = item
	a.mu.Unlock()

	if err := a.saveScripts(); err != nil {
		a.mu.Lock()
		delete(a.scripts, script.ID)
		a.mu.Unlock()
		return ScriptView{}, err
	}

	return toScriptView(item), nil
}

func (a *App) updateScript(id string, payload ScriptPayload) (ScriptView, error) {
	validated, err := validatePayload(payload)
	if err != nil {
		return ScriptView{}, err
	}

	a.mu.Lock()
	item, ok := a.scripts[id]
	if !ok {
		a.mu.Unlock()
		return ScriptView{}, errors.New("脚本不存在")
	}
	running := item.Status == "running"

	item.Config.Name = validated.Name
	item.Config.WorkDir = validated.WorkDir
	item.Config.Content = validated.Content
	item.Config.AutoStart = validated.AutoStart
	item.Config.UpdatedAt = time.Now()
	view := toScriptView(item)
	a.mu.Unlock()

	if err := a.saveScripts(); err != nil {
		return ScriptView{}, err
	}

	if running {
		a.appendLog(id, "system", "脚本配置已更新，运行中的进程不受影响，重启后生效")
	}

	return view, nil
}

func (a *App) deleteScript(id string) error {
	if _, err := a.stopScriptAndWait(id, true); err != nil && err.Error() != "脚本当前未运行" {
		return err
	}

	a.mu.Lock()
	if _, ok := a.scripts[id]; !ok {
		a.mu.Unlock()
		return errors.New("脚本不存在")
	}
	delete(a.scripts, id)
	a.mu.Unlock()

	_ = os.Remove(a.scriptPath(id))
	return a.saveScripts()
}

func (a *App) getScript(id string) (*ManagedScript, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	item, ok := a.scripts[id]
	return item, ok
}

func (a *App) scriptPath(id string) string {
	return filepath.Join(a.scriptFiles, id+".sh")
}

func (a *App) appendLog(id, source, message string) {
	a.mu.Lock()
	item, ok := a.scripts[id]
	if !ok {
		a.mu.Unlock()
		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Source:    source,
		Message:   message,
	}

	item.Logs = append(item.Logs, entry)
	if len(item.Logs) > maxBufferedLogLines {
		item.Logs = item.Logs[len(item.Logs)-maxBufferedLogLines:]
	}

	subscribers := make([]chan LogEntry, 0, len(item.Subscribers))
	for subscriber := range item.Subscribers {
		subscribers = append(subscribers, subscriber)
	}
	a.mu.Unlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- entry:
		default:
		}
	}
}

func (a *App) startScript(id string) (ScriptView, error) {
	a.mu.Lock()
	item, ok := a.scripts[id]
	if !ok {
		a.mu.Unlock()
		return ScriptView{}, errors.New("脚本不存在")
	}
	if item.Status == "running" {
		view := toScriptView(item)
		a.mu.Unlock()
		return view, errors.New("脚本已在运行中")
	}

	scriptPath := a.scriptPath(id)
	if err := os.WriteFile(scriptPath, []byte(item.Config.Content), 0755); err != nil {
		a.mu.Unlock()
		return ScriptView{}, err
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = item.Config.WorkDir
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.mu.Unlock()
		return ScriptView{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		a.mu.Unlock()
		return ScriptView{}, err
	}
	if err := cmd.Start(); err != nil {
		a.mu.Unlock()
		return ScriptView{}, err
	}

	now := time.Now()
	item.Cmd = cmd
	item.Status = "running"
	item.PID = cmd.Process.Pid
	item.StartedAt = &now
	view := toScriptView(item)
	a.mu.Unlock()

	a.appendLog(id, "system", fmt.Sprintf("脚本已启动，PID=%d", view.PID))
	go a.consumePipe(id, "stdout", stdout)
	go a.consumePipe(id, "stderr", stderr)
	go a.watchProcess(id, cmd)

	return view, nil
}

func (a *App) consumePipe(id, source string, reader io.ReadCloser) {
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	for scanner.Scan() {
		a.appendLog(id, source, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "file already closed") {
			return
		}
		a.appendLog(id, "system", fmt.Sprintf("%s 读取失败: %v", source, err))
	}
}

func (a *App) watchProcess(id string, cmd *exec.Cmd) {
	err := cmd.Wait()

	a.mu.Lock()
	item, ok := a.scripts[id]
	if !ok {
		a.mu.Unlock()
		return
	}
	item.Cmd = nil
	item.PID = 0
	item.StartedAt = nil
	item.Status = "stopped"
	a.mu.Unlock()

	if err != nil {
		a.appendLog(id, "system", fmt.Sprintf("脚本退出，错误: %v", err))
		return
	}

	a.appendLog(id, "system", "脚本已退出")
}

func (a *App) getRunningProcess(id string) (*exec.Cmd, ScriptView, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	item, ok := a.scripts[id]
	if !ok {
		return nil, ScriptView{}, errors.New("脚本不存在")
	}
	if item.Status != "running" || item.Cmd == nil || item.Cmd.Process == nil {
		view := toScriptView(item)
		return nil, view, errors.New("脚本当前未运行")
	}

	return item.Cmd, toScriptView(item), nil
}

func (a *App) stopScript(id string) (ScriptView, error) {
	cmd, view, err := a.getRunningProcess(id)
	if err != nil {
		return view, err
	}

	a.appendLog(id, "system", "收到停止请求")

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return view, err
	}

	return view, nil
}

func (a *App) killScript(id string) (ScriptView, error) {
	cmd, view, err := a.getRunningProcess(id)
	if err != nil {
		return view, err
	}

	a.appendLog(id, "system", "收到强制结束请求")
	if err := cmd.Process.Kill(); err != nil {
		return view, err
	}

	return view, nil
}

func (a *App) waitUntilStopped(id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		a.mu.RLock()
		item, ok := a.scripts[id]
		stopped := !ok || item.Status != "running"
		a.mu.RUnlock()

		if stopped {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("等待脚本停止超时")
		}

		<-ticker.C
	}
}

func (a *App) stopScriptAndWait(id string, forceAfterTimeout bool) (ScriptView, error) {
	view, err := a.stopScript(id)
	if err != nil {
		return view, err
	}

	if waitErr := a.waitUntilStopped(id, 3*time.Second); waitErr == nil {
		return view, nil
	}

	if !forceAfterTimeout {
		return view, errors.New("脚本未在超时时间内退出")
	}

	a.appendLog(id, "system", "脚本未在超时时间内退出，准备强制结束")
	if _, err := a.killScript(id); err != nil && err.Error() != "脚本当前未运行" {
		return view, err
	}

	if err := a.waitUntilStopped(id, 2*time.Second); err != nil {
		return view, err
	}

	return view, nil
}

func (a *App) getLogs(id string) ([]LogEntry, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	item, ok := a.scripts[id]
	if !ok {
		return nil, errors.New("脚本不存在")
	}

	logs := make([]LogEntry, len(item.Logs))
	copy(logs, item.Logs)
	return logs, nil
}

func (a *App) subscribe(id string) (chan LogEntry, []LogEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	item, ok := a.scripts[id]
	if !ok {
		return nil, nil, errors.New("脚本不存在")
	}

	ch := make(chan LogEntry, 64)
	item.Subscribers[ch] = struct{}{}

	logs := make([]LogEntry, len(item.Logs))
	copy(logs, item.Logs)
	return ch, logs, nil
}

func (a *App) unsubscribe(id string, ch chan LogEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()

	item, ok := a.scripts[id]
	if !ok {
		close(ch)
		return
	}

	delete(item.Subscribers, ch)
	close(ch)
}

func (a *App) startAutoScripts() {
	a.mu.RLock()
	ids := make([]string, 0, len(a.scripts))
	for id, item := range a.scripts {
		if item.Config.AutoStart {
			ids = append(ids, id)
		}
	}
	a.mu.RUnlock()

	sort.Strings(ids)
	for _, id := range ids {
		if _, err := a.startScript(id); err != nil {
			fmt.Printf("auto start failed for %s: %v\n", id, err)
		}
	}
}

func writeSSE(w io.Writer, entry LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func main() {
	serviceInstall := flag.String("service-install", "", "Install as a system service (systemd or openrc)")
	flag.Parse()

	if *serviceInstall != "" {
		if err := installService(*serviceInstall); err != nil {
			fmt.Fprintf(os.Stderr, "Error installing service: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	config, storePath, scriptFiles, err := setupEnvironment()
	if err != nil {
		panic(err)
	}

	app, err := newApp(config, storePath, scriptFiles)
	if err != nil {
		panic(err)
	}

	r := gin.Default()
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		// c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	api := r.Group("/api")
	{
		api.GET("/config", func(c *gin.Context) {
			c.JSON(http.StatusOK, config)
		})

		api.GET("/scripts", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"scripts": app.listScripts()})
		})

		api.POST("/scripts", func(c *gin.Context) {
			var payload ScriptPayload
			if err := c.ShouldBindJSON(&payload); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式不正确"})
				return
			}

			script, err := app.createScript(payload)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusCreated, gin.H{"script": script})
		})

		api.PUT("/scripts/:id", func(c *gin.Context) {
			var payload ScriptPayload
			if err := c.ShouldBindJSON(&payload); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式不正确"})
				return
			}

			script, err := app.updateScript(c.Param("id"), payload)
			if err != nil {
				status := http.StatusBadRequest
				if err.Error() == "脚本不存在" {
					status = http.StatusNotFound
				}
				c.JSON(status, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"script": script})
		})

		api.DELETE("/scripts/:id", func(c *gin.Context) {
			err := app.deleteScript(c.Param("id"))
			if err != nil {
				status := http.StatusBadRequest
				if err.Error() == "脚本不存在" {
					status = http.StatusNotFound
				}
				c.JSON(status, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
		})

		api.POST("/scripts/:id/start", func(c *gin.Context) {
			script, err := app.startScript(c.Param("id"))
			if err != nil {
				status := http.StatusBadRequest
				if err.Error() == "脚本不存在" {
					status = http.StatusNotFound
				}
				if err.Error() == "脚本已在运行中" {
					status = http.StatusConflict
				}
				c.JSON(status, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"script": script})
		})

		api.POST("/scripts/:id/stop", func(c *gin.Context) {
			script, err := app.stopScript(c.Param("id"))
			if err != nil {
				status := http.StatusBadRequest
				if err.Error() == "脚本不存在" {
					status = http.StatusNotFound
				}
				if err.Error() == "脚本当前未运行" {
					status = http.StatusConflict
				}
				c.JSON(status, gin.H{"error": err.Error(), "script": script})
				return
			}

			c.JSON(http.StatusOK, gin.H{"script": script})
		})

		api.POST("/scripts/:id/kill", func(c *gin.Context) {
			script, err := app.killScript(c.Param("id"))
			if err != nil {
				status := http.StatusBadRequest
				if err.Error() == "脚本不存在" {
					status = http.StatusNotFound
				}
				if err.Error() == "脚本当前未运行" {
					status = http.StatusConflict
				}
				c.JSON(status, gin.H{"error": err.Error(), "script": script})
				return
			}

			c.JSON(http.StatusOK, gin.H{"script": script})
		})

		api.GET("/scripts/:id/logs", func(c *gin.Context) {
			logs, err := app.getLogs(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"logs": logs})
		})

		api.GET("/scripts/:id/logs/stream", func(c *gin.Context) {
			subscriber, history, err := app.subscribe(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			defer app.unsubscribe(c.Param("id"), subscriber)

			c.Writer.Header().Set("Content-Type", "text/event-stream")
			c.Writer.Header().Set("Cache-Control", "no-cache")
			c.Writer.Header().Set("Connection", "keep-alive")

			flusher, ok := c.Writer.(http.Flusher)
			if !ok {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "流式日志当前不可用"})
				return
			}

			for _, entry := range history {
				if err := writeSSE(c.Writer, entry); err != nil {
					return
				}
			}
			flusher.Flush()

			keepAlive := time.NewTicker(15 * time.Second)
			defer keepAlive.Stop()

			for {
				select {
				case entry := <-subscriber:
					if err := writeSSE(c.Writer, entry); err != nil {
						return
					}
					flusher.Flush()
				case <-keepAlive.C:
					if _, err := fmt.Fprint(c.Writer, ": keep-alive\n\n"); err != nil {
						return
					}
					flusher.Flush()
				case <-c.Request.Context().Done():
					return
				}
			}
		})

		api.GET("/service/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, getServiceStatus())
		})

		api.POST("/service/install", func(c *gin.Context) {
			var payload struct {
				Type string `json:"type"`
			}
			if err := c.ShouldBindJSON(&payload); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
				return
			}

			if err := installService(payload.Type); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": "服务配置已生成，请按提示完成安装"})
		})
	}

	// 静态文件服务 (SPA 支持)
	staticFS, _ := fs.Sub(frontendContent, "dist")
	staticServer := http.FileServer(http.FS(staticFS))

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// 检查文件是否存在于嵌入的 FS 中
		if _, err := staticFS.Open(strings.TrimPrefix(path, "/")); err == nil {
			staticServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// 如果不存在且不是 API 请求，返回 index.html
		if !strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, "/ping") {
			c.Request.URL.Path = "/"
			staticServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
	})

	app.startAutoScripts()
	address := fmt.Sprintf(":%d", config.Port)
	fmt.Printf("%s listening on %s\n", config.Name, address)
	if err := r.Run(address); err != nil {
		panic(err)
	}
}
