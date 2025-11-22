import { useState, useEffect } from 'react';
import './App.css';

function App() {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    checkAuth();
  }, []);

  const checkAuth = async () => {
    try {
      const res = await fetch('/api/check-auth');
      if (res.ok) {
        setIsLoggedIn(true);
      } else {
        setIsLoggedIn(false);
      }
    } catch (err) {
      console.error("Auth check failed", err);
      setIsLoggedIn(false);
    } finally {
      setIsLoading(false);
    }
  };

  const handleLogin = async (e) => {
    e.preventDefault();
    setError('');
    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ password }),
      });

      if (res.ok) {
        setIsLoggedIn(true);
        setPassword('');
      } else {
        setError('Invalid password');
      }
    } catch (err) {
      setError('Login failed');
    }
  };

  const [entryContent, setEntryContent] = useState('');
  const [entryStatus, setEntryStatus] = useState('');

  const handleEntrySubmit = async (e) => {
    e.preventDefault();
    setEntryStatus('Sending...');
    try {
      const res = await fetch('/api/entries', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ content: entryContent }),
      });

      if (res.ok) {
        setEntryStatus('Entry saved!');
        setEntryContent('');
        setTimeout(() => setEntryStatus(''), 3000);
      } else {
        setEntryStatus('Failed to save entry.');
      }
    } catch (err) {
      console.error("Entry submission failed", err);
      setEntryStatus('Error saving entry.');
    }
  };

  if (isLoading) {
    return <div className="App loading">Loading...</div>;
  }

  if (!isLoggedIn) {
    return (
      <div className="App login-container">
        <form onSubmit={handleLogin} className="login-form">
          <h1>Journal Login</h1>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Enter Password"
            className="password-input"
          />
          <button type="submit" className="login-button">Login</button>
          {error && <p className="error-message">{error}</p>}
        </form>
      </div>
    );
  }

  return (
    <div className="App">
      <header className="App-header">
        <h1>Quick Journal</h1>
        <form onSubmit={handleEntrySubmit} className="entry-form">
          <textarea
            value={entryContent}
            onChange={(e) => setEntryContent(e.target.value)}
            placeholder="Write your thoughts..."
            className="entry-textarea"
            rows={10}
          />
          <div className="form-footer">
            <button type="submit" className="submit-button" disabled={!entryContent.trim()}>
              Save Entry
            </button>
            {entryStatus && <span className="status-message">{entryStatus}</span>}
          </div>
        </form>
      </header>
    </div>
  );
}

export default App;
