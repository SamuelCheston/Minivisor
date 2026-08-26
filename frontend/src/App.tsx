import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  AppBar,
  Box,
  Button,
  Chip,
  CircularProgress,
  Container,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControlLabel,
  List,
  ListItemButton,
  ListItemText,
  Paper,
  Stack,
  Switch,
  TextField,
  Toolbar,
  Typography,
} from '@mui/material'

type Script = {
  id: string
  name: string
  workDir: string
  content: string
  autoStart: boolean
  createdAt: string
  updatedAt: string
  status: string
  pid?: number
  startedAt?: string
}

type LogEntry = {
  timestamp: string
  source: string
  message: string
}

type ScriptForm = {
  name: string
  workDir: string
  content: string
  autoStart: boolean
}

type Config = {
  port: number
  name: string
}

type ServiceStatus = {
  type: string
  installed: boolean
  userExists: boolean
  unitPath: string
  canInstall: boolean
}

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

const emptyForm: ScriptForm = {
  name: '',
  workDir: '',
  content: '',
  autoStart: false,
}

function App() {
  const [config, setConfig] = useState<Config | null>(null)
  const [serviceStatus, setServiceStatus] = useState<ServiceStatus | null>(null)
  const [serviceDialogOpen, setServiceDialogOpen] = useState(false)
  const [scripts, setScripts] = useState<Script[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [form, setForm] = useState<ScriptForm>(emptyForm)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [actioning, setActioning] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('后端连接中...')
  const logContainerRef = useRef<HTMLDivElement | null>(null)

  const selectedScript = useMemo(
    () => scripts.find((script) => script.id === selectedId) ?? null,
    [scripts, selectedId],
  )

  async function fetchJson<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(`${API_BASE}${path}`, {
      headers: {
        'Content-Type': 'application/json',
      },
      ...init,
    })

    const data = await response.json().catch(() => ({}))
    if (!response.ok) {
      throw new Error(data.error ?? '请求失败')
    }

    return data as T
  }

  async function loadScripts() {
    const data = await fetchJson<{ scripts: Script[] }>('/api/scripts')
    setScripts(data.scripts)
    setNotice('后端已连接')

    setSelectedId((current) => {
      if (current && data.scripts.some((script) => script.id === current)) {
        return current
      }
      return null
    })
  }

  async function loadConfig() {
    try {
      const data = await fetchJson<Config>('/api/config')
      setConfig(data)
    } catch (err) {
      console.error('Failed to load config:', err)
    }
  }

  async function loadServiceStatus() {
    try {
      const data = await fetchJson<ServiceStatus>('/api/service/status')
      setServiceStatus(data)
    } catch (err) {
      console.error('Failed to load service status:', err)
    }
  }

  async function handleInstallService() {
    if (!serviceStatus?.type || serviceStatus.type === 'none') return
    
    setActioning(true)
    setError('')
    try {
      const data = await fetchJson<{ message: string }>('/api/service/install', {
        method: 'POST',
        body: JSON.stringify({ type: serviceStatus.type }),
      })
      alert(data.message)
      await loadServiceStatus()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setActioning(false)
    }
  }

  async function loadLogs(id: string) {
    const data = await fetchJson<{ logs: LogEntry[] }>(`/api/scripts/${id}/logs`)
    setLogs(data.logs)
  }

  useEffect(() => {
    loadConfig()
    loadServiceStatus()
    loadScripts()
      .catch((err: Error) => {
        setError(err.message)
        setNotice('后端连接失败')
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!selectedScript) {
      setForm(emptyForm)
      return
    }

    setForm({
      name: selectedScript.name,
      workDir: selectedScript.workDir,
      content: selectedScript.content,
      autoStart: selectedScript.autoStart,
    })
  }, [selectedScript])

  useEffect(() => {
    const timer = window.setInterval(() => {
      loadScripts().catch(() => undefined)
    }, 3000)

    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    if (!selectedId) {
      setLogs([])
      return
    }

    let active = true
    const stream = new EventSource(`${API_BASE}/api/scripts/${selectedId}/logs/stream`)

    loadLogs(selectedId).catch(() => undefined)

    stream.onmessage = (event) => {
      if (!active) {
        return
      }

      const entry = JSON.parse(event.data) as LogEntry
      setLogs((current) => {
        const next = [...current, entry]
        return next.slice(-500)
      })
      loadScripts().catch(() => undefined)
    }

    stream.onerror = () => {
      stream.close()
    }

    return () => {
      active = false
      stream.close()
    }
  }, [selectedId])

  useEffect(() => {
    if (!logContainerRef.current) {
      return
    }

    logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
  }, [logs])

  function updateForm<K extends keyof ScriptForm>(key: K, value: ScriptForm[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  async function handleSave() {
    setSaving(true)
    setError('')

    try {
      if (selectedScript) {
        await fetchJson<{ script: Script }>(`/api/scripts/${selectedScript.id}`, {
          method: 'PUT',
          body: JSON.stringify(form),
        })
      } else {
        const data = await fetchJson<{ script: Script }>('/api/scripts', {
          method: 'POST',
          body: JSON.stringify(form),
        })
        setSelectedId(data.script.id)
      }

      await loadScripts()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  async function handleStart() {
    if (!selectedScript) {
      return
    }

    setActioning(true)
    setError('')
    try {
      await fetchJson(`/api/scripts/${selectedScript.id}/start`, { method: 'POST' })
      await loadScripts()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setActioning(false)
    }
  }

  async function handleStop() {
    if (!selectedScript) {
      return
    }

    setActioning(true)
    setError('')
    try {
      await fetchJson(`/api/scripts/${selectedScript.id}/stop`, { method: 'POST' })
      await loadScripts()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setActioning(false)
    }
  }

  async function handleKill() {
    if (!selectedScript) {
      return
    }

    setActioning(true)
    setError('')
    try {
      await fetchJson(`/api/scripts/${selectedScript.id}/kill`, { method: 'POST' })
      await loadScripts()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setActioning(false)
    }
  }

  async function handleDelete() {
    if (!selectedScript || !window.confirm(`确认删除脚本“${selectedScript.name}”吗？`)) {
      return
    }

    setActioning(true)
    setError('')
    try {
      await fetchJson(`/api/scripts/${selectedScript.id}`, { method: 'DELETE' })
      setSelectedId(null)
      setLogs([])
      await loadScripts()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setActioning(false)
    }
  }

  function handleCreateNew() {
    setSelectedId(null)
    setForm(emptyForm)
    setLogs([])
    setError('')
  }

  return (
    <Box sx={{ minHeight: '100vh', backgroundColor: '#f5f7fb' }}>
      <AppBar position="static">
        <Toolbar>
          <Typography variant="h6" sx={{ flexGrow: 1 }}>
            {config?.name ?? 'Minivisor'}
          </Typography>
          <Button
            color="inherit"
            onClick={() => {
              loadServiceStatus()
              setServiceDialogOpen(true)
            }}
            sx={{ mr: 2 }}
          >
            服务管理
          </Button>
          <Chip
            label={notice}
            color={notice.includes('失败') ? 'error' : 'success'}
            variant="outlined"
            sx={{ color: 'white', borderColor: 'rgba(255,255,255,0.4)' }}
          />
        </Toolbar>
      </AppBar>

      <Container maxWidth={false} sx={{ py: 3 }}>
        <Stack
          direction={{ xs: 'column', lg: 'row' }}
          spacing={3}
          sx={{ alignItems: 'stretch' }}
        >
          <Paper elevation={2} sx={{ width: { xs: '100%', lg: 320 }, p: 2 }}>
            <Stack spacing={2}>
              <Stack
                direction="row"
                sx={{ justifyContent: 'space-between', alignItems: 'center' }}
              >
                <Typography variant="h6">脚本列表</Typography>
                <Button variant="contained" size="small" onClick={handleCreateNew}>
                  新建脚本
                </Button>
              </Stack>

              {loading ? (
                <Box sx={{ py: 6, textAlign: 'center' }}>
                  <CircularProgress size={28} />
                </Box>
              ) : (
                <List disablePadding sx={{ border: '1px solid #e0e4ea', borderRadius: 2 }}>
                  {scripts.length === 0 ? (
                    <Box sx={{ p: 2 }}>
                      <Typography color="text.secondary">还没有脚本，先创建一个。</Typography>
                    </Box>
                  ) : (
                    scripts.map((script, index) => (
                      <Box key={script.id}>
                        <ListItemButton
                          selected={script.id === selectedId}
                          onClick={() => setSelectedId(script.id)}
                          alignItems="flex-start"
                        >
                          <ListItemText
                            primary={script.name}
                            secondary={
                              <Stack spacing={0.5} sx={{ mt: 0.5 }}>
                                <Box>
                                  <Chip
                                    size="small"
                                    label={script.status === 'running' ? '运行中' : '已停止'}
                                    color={script.status === 'running' ? 'success' : 'default'}
                                  />
                                </Box>
                                <Typography variant="caption" color="text.secondary">
                                  {script.workDir}
                                </Typography>
                                <Typography variant="caption" color="text.secondary">
                                  {script.autoStart ? '服务启动时自动运行' : '手动启动'}
                                </Typography>
                              </Stack>
                            }
                          />
                        </ListItemButton>
                        {index < scripts.length - 1 && <Divider />}
                      </Box>
                    ))
                  )}
                </List>
              )}
            </Stack>
          </Paper>

          <Stack spacing={3} sx={{ flex: 1 }}>
            {error ? <Alert severity="error">{error}</Alert> : null}

            <Paper elevation={2} sx={{ p: 3 }}>
              <Stack spacing={2}>
                <Box>
                  <Typography variant="h6">
                    {selectedScript ? `编辑脚本：${selectedScript.name}` : '创建脚本'}
                  </Typography>
                  <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                    当前按 Shell 脚本方式执行，脚本会使用 bash 在指定目录中运行。
                  </Typography>
                  {selectedScript?.status === 'running' ? (
                    <Typography variant="body2" color="warning.main" sx={{ mt: 0.5 }}>
                      脚本正在运行，当前保存的修改会在下次启动时生效。
                    </Typography>
                  ) : null}
                </Box>

                <TextField
                  label="脚本名称"
                  value={form.name}
                  onChange={(event) => updateForm('name', event.target.value)}
                  fullWidth
                />

                <TextField
                  label="运行目录"
                  value={form.workDir}
                  onChange={(event) => updateForm('workDir', event.target.value)}
                  fullWidth
                  placeholder="/home/lnb/project"
                />

                <TextField
                  label="脚本内容"
                  value={form.content}
                  onChange={(event) => updateForm('content', event.target.value)}
                  fullWidth
                  multiline
                  minRows={12}
                  placeholder={'echo "hello"\npwd\nls -la'}
                />

                <FormControlLabel
                  control={
                    <Switch
                      checked={form.autoStart}
                      onChange={(event) => updateForm('autoStart', event.target.checked)}
                    />
                  }
                  label="随服务启动自动运行"
                />

                <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5}>
                  <Button variant="contained" onClick={handleSave} disabled={saving}>
                    {saving ? '保存中...' : selectedScript ? '保存修改' : '创建脚本'}
                  </Button>
                  <Button
                    variant="outlined"
                    onClick={handleStart}
                    disabled={!selectedScript || actioning || selectedScript.status === 'running'}
                  >
                    启动
                  </Button>
                  <Button
                    variant="outlined"
                    color="warning"
                    onClick={handleStop}
                    disabled={!selectedScript || actioning || selectedScript.status !== 'running'}
                  >
                    优雅停止
                  </Button>
                  <Button
                    variant="outlined"
                    color="error"
                    onClick={handleKill}
                    disabled={!selectedScript || actioning || selectedScript.status !== 'running'}
                  >
                    Kill
                  </Button>
                  <Button
                    variant="outlined"
                    color="inherit"
                    onClick={handleDelete}
                    disabled={!selectedScript || actioning}
                  >
                    停止并删除
                  </Button>
                </Stack>

                {selectedScript ? (
                  <Stack direction={{ xs: 'column', md: 'row' }} spacing={1.5}>
                    <Chip
                      label={selectedScript.status === 'running' ? '状态：运行中' : '状态：已停止'}
                      color={selectedScript.status === 'running' ? 'success' : 'default'}
                    />
                    {selectedScript.pid ? <Chip label={`PID: ${selectedScript.pid}`} /> : null}
                    <Chip
                      label={
                        selectedScript.autoStart ? '启动策略：自动运行' : '启动策略：手动运行'
                      }
                    />
                  </Stack>
                ) : null}
              </Stack>
            </Paper>

            <Paper elevation={2} sx={{ p: 3 }}>
              <Stack spacing={2}>
                <Box>
                  <Typography variant="h6">实时日志</Typography>
                  <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                    这里会显示脚本的 stdout、stderr 和系统事件。
                  </Typography>
                </Box>

                <Box
                  ref={logContainerRef}
                  sx={{
                    height: 360,
                    overflowY: 'auto',
                    borderRadius: 2,
                    backgroundColor: '#111827',
                    color: '#e5e7eb',
                    p: 2,
                    fontFamily: 'ui-monospace, SFMono-Regular, monospace',
                    fontSize: 13,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                  }}
                >
                  {selectedId ? (
                    logs.length > 0 ? (
                      logs.map((entry, index) => (
                        <Box key={`${entry.timestamp}-${index}`} sx={{ mb: 1 }}>
                          <Box component="span" sx={{ color: '#9ca3af' }}>
                            [{new Date(entry.timestamp).toLocaleTimeString()}]
                          </Box>{' '}
                          <Box
                            component="span"
                            sx={{
                              color:
                                entry.source === 'stderr'
                                  ? '#fca5a5'
                                  : entry.source === 'system'
                                    ? '#93c5fd'
                                    : '#86efac',
                            }}
                          >
                            {entry.source}
                          </Box>{' '}
                          <Box component="span">{entry.message}</Box>
                        </Box>
                      ))
                    ) : (
                      <Typography color="#9ca3af">还没有日志输出。</Typography>
                    )
                  ) : (
                    <Typography color="#9ca3af">选择一个脚本后可查看日志。</Typography>
                  )}
                </Box>
              </Stack>
            </Paper>
          </Stack>
        </Stack>
      </Container>

      <Dialog
        open={serviceDialogOpen}
        onClose={() => setServiceDialogOpen(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>服务管理 (systemd / OpenRC)</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <Box>
              <Typography variant="subtitle2" gutterBottom>
                当前系统环境
              </Typography>
              <Typography variant="body2" color="text.secondary">
                检测到的初始化系统：
                <Box component="span" sx={{ fontWeight: 'bold', ml: 1, color: 'primary.main' }}>
                  {serviceStatus?.type === 'none' ? '未检测到' : serviceStatus?.type}
                </Box>
              </Typography>
            </Box>

            <Divider />

            <Box>
              <Typography variant="subtitle2" gutterBottom>
                安装状态
              </Typography>
              {serviceStatus?.installed ? (
                <Alert severity="success" sx={{ py: 0 }}>
                  服务已安装在：{serviceStatus.unitPath}
                </Alert>
              ) : (
                <Alert severity="info" sx={{ py: 0 }}>
                  服务尚未安装为系统自启动服务。
                </Alert>
              )}
            </Box>

            <Box>
              <Typography variant="subtitle2" gutterBottom>
                专用用户检查
              </Typography>
              {serviceStatus?.userExists ? (
                <Typography variant="body2" color="success.main">
                  ✓ 用户 'minivisor' 已存在。
                </Typography>
              ) : (
                <Typography variant="body2" color="warning.main">
                  ⚠ 用户 'minivisor' 不存在。安装前请手动创建：
                  <Box
                    component="pre"
                    sx={{
                      p: 1,
                      backgroundColor: '#f0f0f0',
                      borderRadius: 1,
                      fontSize: 12,
                      mt: 1,
                    }}
                  >
                    sudo useradd -r -s /bin/false minivisor
                  </Box>
                </Typography>
              )}
            </Box>

            {!serviceStatus?.installed && serviceStatus?.canInstall && (
              <Box>
                <Typography variant="subtitle2" gutterBottom>
                  操作建议
                </Typography>
                <Typography variant="body2">
                  点击下方按钮生成配置文件。由于需要写入系统目录，如果当前进程权限不足，后端会提示你使用
                  sudo 命令在终端完成。
                </Typography>
              </Box>
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setServiceDialogOpen(false)}>关闭</Button>
          {!serviceStatus?.installed && serviceStatus?.canInstall && (
            <Button
              variant="contained"
              onClick={handleInstallService}
              disabled={actioning}
            >
              生成配置并安装
            </Button>
          )}
        </DialogActions>
      </Dialog>
    </Box>
  )
}

export default App
