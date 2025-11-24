import { useState, useEffect } from 'react';
import './App.css';

function App() {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState('');
  const [view, setView] = useState('new');
  const [pastEntries, setPastEntries] = useState([]);
  const [isLoadingEntries, setIsLoadingEntries] = useState(false);

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

  const fetchEntries = async () => {
    setIsLoadingEntries(true);
    try {
      const res = await fetch('/api/entries');
      if (res.ok) {
        const data = await res.json();
        const rawContent = data.content || '';
        const entries = parseEntries(rawContent);
        setPastEntries(entries);
      }
    } catch (err) {
      console.error("Failed to fetch entries", err);
    } finally {
      setIsLoadingEntries(false);
    }
  };

  const parseEntries = (content) => {
    const lines = content.split('\n');
    const entries = [];
    let currentEntry = null;

    lines.forEach(line => {
      if (line.startsWith('* 20')) {
        if (currentEntry) {
          entries.push(currentEntry);
        }
        currentEntry = { date: line.substring(2), content: '' };
      } else if (currentEntry) {
        currentEntry.content += line + '\n';
      }
    });
    if (currentEntry) {
      entries.push(currentEntry);
    }
    return entries.reverse();
  };

  useEffect(() => {
    if (isLoggedIn && view === 'past') {
      fetchEntries();
    }
  }, [isLoggedIn, view]);

  if (isLoading) {
    return <div className="App loading">Loading...</div>;
  }

  if (!isLoggedIn) {
    return (
      <div className="App login-container">
        <form onSubmit={handleLogin} className="login-form">
          <h1>Journal Login</h1>
          <div className="password-container">
            <input
              type={showPassword ? "text" : "password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Enter Password"
              className="password-input"
            />
            <button
              type="button"
              className="password-toggle"
              onClick={() => setShowPassword(!showPassword)}
            >
              {showPassword ? "👁️" : "👁️‍🗨️"}
            </button>
          </div>
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

        <nav className="nav-bar">
          <button
            className={`nav-tab ${view === 'new' ? 'active' : ''}`}
            onClick={() => setView('new')}
          >
            New Entry
          </button>
          <button
            className={`nav-tab ${view === 'past' ? 'active' : ''}`}
            onClick={() => setView('past')}
          >
            Past Entries
          </button>
        </nav>

        {view === 'new' ? (
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
        ) : (
          <div className="entries-container">
            {isLoadingEntries ? (
              <p>Loading entries...</p>
            ) : pastEntries.length === 0 ? (
              <p>No past entries found.</p>
            ) : (
              pastEntries.map((entry, index) => (
                <div key={index} className="entry-card">
                  <div className="entry-header">{entry.date}</div>
                  <div className="entry-content">{entry.content}</div>
                </div>
              ))
            )}
          </div>
        )}
      </header>
    </div>
  );
}

export default App;
