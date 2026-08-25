import { useState, useEffect } from 'react'
import { 
  Container, 
  Typography, 
  Box, 
  Button, 
  AppBar, 
  Toolbar, 
  Paper,
  CircularProgress
} from '@mui/material'

function App() {
  const [message, setMessage] = useState<string>('')
  const [loading, setLoading] = useState<boolean>(true)

  useEffect(() => {
    fetch('http://localhost:8080/api/hello')
      .then(res => res.json())
      .then(data => {
        setMessage(data.message)
        setLoading(false)
      })
      .catch(err => {
        console.error(err)
        setMessage('Failed to connect to backend')
        setLoading(false)
      })
  }, [])

  return (
    <Box sx={{ flexGrow: 1 }}>
      <AppBar position="static">
        <Toolbar>
          <Typography variant="h6" component="div" sx={{ flexGrow: 1 }}>
            Minivisor Dashboard
          </Typography>
        </Toolbar>
      </AppBar>
      <Container maxWidth="sm" sx={{ mt: 4 }}>
        <Paper elevation={3} sx={{ p: 4, textAlign: 'center' }}>
          <Typography variant="h4" gutterBottom color="primary">
            Welcome to Minivisor
          </Typography>
          <Typography variant="body1" sx={{ mb: 3 }}>
            This project is initialized with Go (Gin) and React (Material UI).
          </Typography>
          
          <Box sx={{ my: 4 }}>
            <Typography variant="h6">Backend Status:</Typography>
            {loading ? (
              <CircularProgress size={24} sx={{ mt: 1 }} />
            ) : (
              <Typography variant="body1" color={message.includes('Failed') ? 'error' : 'success.main'}>
                {message}
              </Typography>
            )}
          </Box>

          <Button variant="contained" color="primary" onClick={() => window.location.reload()}>
            Refresh Status
          </Button>
        </Paper>
      </Container>
    </Box>
  )
}

export default App
