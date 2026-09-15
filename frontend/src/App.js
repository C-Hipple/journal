import { useState, useEffect, useCallback } from 'react';
import './App.css';

// Phone photos are far larger than a note needs to be, and every one of them is
// committed to the notes repo, so captures are downscaled before upload.
const MAX_PHOTO_EDGE = 1600;
const PHOTO_QUALITY = 0.85;

// The topic is remembered in a cookie so a talk's name is typed once, not once
// per note. Keep the cap in step with sanitizeTopic in storage.go: a topic the
// server would truncate anyway is not worth carrying around in a cookie.
const TOPIC_COOKIE = 'journal_topic';
const MAX_TOPIC_LENGTH = 120;
// Where the topic used to be kept, cleared on load so it doesn't linger.
const LEGACY_TOPIC_STORAGE_KEY = 'journal.topic';

function App() {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState('');
  const [view, setView] = useState('new');
  const [pastEntries, setPastEntries] = useState([]);
  const [isLoadingEntries, setIsLoadingEntries] = useState(false);
  const [entryTypes, setEntryTypes] = useState([{ id: 'journal', name: 'Journal' }]);
  const [selectedType, setSelectedType] = useState('journal');
  const [viewType, setViewType] = useState('journal');
  const [topic, setTopic] = useState(() => readStoredTopic());
  const [knownTopics, setKnownTopics] = useState([]);
  const [entryContent, setEntryContent] = useState('');
  const [entryStatus, setEntryStatus] = useState('');
  const [photoStatus, setPhotoStatus] = useState('');
  const [lastPhoto, setLastPhoto] = useState(null);

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

  useEffect(() => {
    if (isLoggedIn) {
      fetchTypes();
    }
  }, [isLoggedIn]);

  const fetchTypes = async () => {
    try {
      const res = await fetch('/api/types');
      if (res.ok) {
        const data = await res.json();
        setEntryTypes(data);
      }
    } catch (err) {
      console.error("Failed to fetch types", err);
    }
  };

  // Today's topics, so a talk can be resumed with a tap instead of retyping it.
  const fetchTopics = useCallback(async () => {
    try {
      const res = await fetch(`/api/topics?type=${encodeURIComponent(selectedType)}`);
      if (res.ok) {
        const data = await res.json();
        setKnownTopics(data.topics || []);
      }
    } catch (err) {
      console.error("Failed to fetch topics", err);
    }
  }, [selectedType]);

  useEffect(() => {
    if (isLoggedIn) {
      fetchTopics();
    }
  }, [isLoggedIn, fetchTopics]);

  // The topic outlives a reload, a locked phone and a new tab, so a talk's
  // notes land together without its name being retyped — but only until
  // midnight, since it is the day's topic and not a standing setting.
  useEffect(() => {
    writeStoredTopic(topic);
  }, [topic]);

  // Drop the topic the localStorage version of this left behind.
  useEffect(() => {
    try {
      window.localStorage.removeItem(LEGACY_TOPIC_STORAGE_KEY);
    } catch (err) {
      // Private browsing and blocked site data: nothing to clean up.
    }
  }, []);

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

  const handleEntrySubmit = async (e) => {
    e.preventDefault();
    setEntryStatus('Sending...');
    try {
      const res = await fetch('/api/entries', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ content: entryContent, type: selectedType, topic: topic.trim() }),
      });

      if (res.ok) {
        setEntryStatus(topic.trim() ? `Added to "${topic.trim()}"` : 'Entry saved!');
        setEntryContent('');
        fetchTopics();
        setTimeout(() => setEntryStatus(''), 3000);
      } else {
        setEntryStatus('Failed to save entry.');
      }
    } catch (err) {
      console.error("Entry submission failed", err);
      setEntryStatus('Error saving entry.');
    }
  };

  const handlePhotoCapture = async (e) => {
    const file = e.target.files && e.target.files[0];
    // Reset the input so the same photo can be picked again if an upload fails.
    e.target.value = '';
    if (!file) {
      return;
    }

    setPhotoStatus('Uploading photo...');
    try {
      const body = new FormData();
      body.append('photo', await downscaleImage(file), 'capture.jpg');
      body.append('type', selectedType);
      body.append('topic', topic.trim());

      const res = await fetch('/api/photos', { method: 'POST', body });
      if (!res.ok) {
        setPhotoStatus('Failed to save photo.');
        return;
      }

      const data = await res.json();
      setLastPhoto(data.url);
      setPhotoStatus(topic.trim() ? `Photo added to "${topic.trim()}"` : 'Photo saved!');
      fetchTopics();
      setTimeout(() => setPhotoStatus(''), 4000);
    } catch (err) {
      console.error("Photo upload failed", err);
      setPhotoStatus('Error saving photo.');
    }
  };

  useEffect(() => {
    const fetchEntries = async () => {
      setIsLoadingEntries(true);
      try {
        const res = await fetch(`/api/entries?type=${viewType}`);
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

    if (isLoggedIn && view === 'past') {
      fetchEntries();
    }
  }, [isLoggedIn, view, viewType]);

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
            <div className="type-selector">
              <label htmlFor="entry-type">Entry Type: </label>
              <select
                id="entry-type"
                value={selectedType}
                onChange={(e) => setSelectedType(e.target.value)}
                className="type-select"
              >
                {entryTypes.map(t => (
                  <option key={t.id} value={t.id}>{t.name}</option>
                ))}
              </select>
            </div>

            <div className="topic-picker">
              <label htmlFor="entry-topic">Topic (e.g. the talk you're in)</label>
              <div className="topic-input-row">
                <input
                  id="entry-topic"
                  type="text"
                  value={topic}
                  onChange={(e) => setTopic(e.target.value)}
                  placeholder="No topic — filed under today"
                  className="topic-input"
                  list="known-topics"
                  autoComplete="off"
                  maxLength={MAX_TOPIC_LENGTH}
                />
                {topic && (
                  <button type="button" className="topic-clear" onClick={() => setTopic('')}>
                    Clear
                  </button>
                )}
              </div>
              <datalist id="known-topics">
                {knownTopics.map(name => (
                  <option key={name} value={name} />
                ))}
              </datalist>
              {knownTopics.length > 0 && (
                <div className="topic-chips">
                  {knownTopics.map(name => (
                    <button
                      key={name}
                      type="button"
                      className={`topic-chip ${topic === name ? 'active' : ''}`}
                      onClick={() => setTopic(name)}
                    >
                      {name}
                    </button>
                  ))}
                </div>
              )}
              <p className="topic-hint">
                {topic.trim()
                  ? 'Notes and photos join this topic, and the summary is rewritten from all of them.'
                  : 'Set a topic to group a talk’s notes together and have them summarised as one.'}
              </p>
            </div>

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
              <label className="photo-button">
                📷 Add Photo
                <input
                  type="file"
                  accept="image/*"
                  capture="environment"
                  onChange={handlePhotoCapture}
                  className="photo-input"
                />
              </label>
              {entryStatus && <span className="status-message">{entryStatus}</span>}
              {photoStatus && <span className="status-message">{photoStatus}</span>}
            </div>
            {lastPhoto && (
              <div className="photo-preview">
                <img src={lastPhoto} alt="Most recent attachment" />
              </div>
            )}
          </form>
        ) : (
          <div className="entries-container">
            <div className="view-type-selector">
              <label htmlFor="view-type">View Type: </label>
              <select
                id="view-type"
                value={viewType}
                onChange={(e) => setViewType(e.target.value)}
                className="type-select"
              >
                {entryTypes.map(t => (
                  <option key={t.id} value={t.id}>{t.name}</option>
                ))}
              </select>
            </div>
            {isLoadingEntries ? (
              <p>Loading entries...</p>
            ) : pastEntries.length === 0 ? (
              <p>No past entries found.</p>
            ) : (
              pastEntries.map((entry, index) => (
                <div key={index} className="entry-card">
                  <div className="entry-header">
                    {entry.date}
                    <RawInputTooltip rawInput={entry.rawInput} />
                  </div>
                  <EntryBody section={entry} />
                  {entry.groups.map((group) => (
                    <div key={group.topic} className="topic-block">
                      <div className="topic-block-header">
                        {group.topic}
                        <RawInputTooltip rawInput={group.rawInput} />
                      </div>
                      <EntryBody section={group} />
                    </div>
                  ))}
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

function RawInputTooltip({ rawInput }) {
  if (!rawInput) {
    return null;
  }
  return (
    <div className="tooltip-container">
      <span className="info-icon">🔍</span>
      <div className="tooltip-content">
        <strong>Raw Input:</strong>
        <pre>{rawInput}</pre>
      </div>
    </div>
  );
}

function EntryBody({ section }) {
  const hasContent = Boolean(section.content);
  return (
    <div className="entry-content">
      {hasContent ? renderContent(section.content) : null}
      {!hasContent && section.rawInput ? (
        <div className="pending-entry">
          <p className="pending-note">Not processed yet — raw notes:</p>
          <p className="raw-text">{section.rawInput}</p>
        </div>
      ) : null}
      {section.photos.length > 0 && (
        <div className="photo-grid">
          {section.photos.map((photo) => (
            <a key={photo.src} href={`/api/media/${photo.src}`} target="_blank" rel="noreferrer">
              <img src={`/api/media/${photo.src}`} alt={photo.caption || 'Attachment'} />
            </a>
          ))}
        </div>
      )}
    </div>
  );
}

// createImageBitmap applies the EXIF orientation a phone camera records; the
// <img> path is the fallback for browsers that don't take the option.
const loadImage = (file) => {
  if (typeof createImageBitmap === 'function') {
    return createImageBitmap(file, { imageOrientation: 'from-image' }).catch(() => loadImageElement(file));
  }
  return loadImageElement(file);
};

const loadImageElement = (file) =>
  new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const img = new Image();
    img.onload = () => {
      URL.revokeObjectURL(url);
      resolve(img);
    };
    img.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error('Could not read that image'));
    };
    img.src = url;
  });

const downscaleImage = async (file) => {
  try {
    const source = await loadImage(file);
    const width = source.naturalWidth || source.width;
    const height = source.naturalHeight || source.height;
    const scale = Math.min(1, MAX_PHOTO_EDGE / Math.max(width, height));

    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(width * scale));
    canvas.height = Math.max(1, Math.round(height * scale));
    canvas.getContext('2d').drawImage(source, 0, 0, canvas.width, canvas.height);
    if (typeof source.close === 'function') {
      source.close();
    }

    const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', PHOTO_QUALITY));
    return blob || file;
  } catch (err) {
    // Better to upload the original than to lose the photo.
    console.error("Could not downscale photo, uploading as captured", err);
    return file;
  }
};

// Midnight tonight, in the phone's own timezone. The topic belongs to a single
// day of notes, so it should not outlive that day.
export const endOfToday = () => {
  const midnight = new Date();
  midnight.setHours(23, 59, 59, 999);
  return midnight;
};

export const readStoredTopic = () => {
  try {
    const prefix = `${TOPIC_COOKIE}=`;
    const entry = document.cookie.split('; ').find((part) => part.startsWith(prefix));
    return entry ? decodeURIComponent(entry.slice(prefix.length)) : '';
  } catch (err) {
    // A cookie we can't read or decode is no reason to fail the form.
    return '';
  }
};

export const writeStoredTopic = (topic) => {
  // Secure is what a browser wants before it keeps a cookie on https, and it
  // must be left off over plain http, which is how `make dev` serves the app.
  const flags = `Path=/; SameSite=Lax${window.location.protocol === 'https:' ? '; Secure' : ''}`;
  try {
    document.cookie = topic
      ? `${TOPIC_COOKIE}=${encodeURIComponent(topic)}; Expires=${endOfToday().toUTCString()}; ${flags}`
      : `${TOPIC_COOKIE}=; Max-Age=0; ${flags}`;
  } catch (err) {
    // Blocked cookies: the topic just won't outlive the page.
  }
};

// headerLevel normalises Markdown and Org headings onto one depth scale, so
// "## "/"* " is a day, "### "/"** " a section or topic, and "#### "/"*** " a
// section inside a topic.
const headerLevel = (line) => {
  const markdown = line.match(/^(#{1,6}) /);
  if (markdown) {
    return markdown[1].length;
  }
  const org = line.match(/^(\*{1,6}) /);
  if (org) {
    return org[1].length + 1;
  }
  return 0;
};

const headerText = (line) => line.slice(line.indexOf(' ') + 1);

const renderContent = (content) => {
  const lines = content.split('\n');
  const elements = [];
  let currentListItems = [];

  const flushList = (keyPrefix) => {
    if (currentListItems.length > 0) {
      elements.push(<ul key={`${keyPrefix}-list`}>{currentListItems}</ul>);
      currentListItems = [];
    }
  };

  lines.forEach((line, index) => {
    const trimmedLine = line.trim();
    if (!trimmedLine) {
      flushList(index);
      return;
    }

    if (trimmedLine.startsWith('- ')) {
      currentListItems.push(<li key={index}>{trimmedLine.substring(2)}</li>);
      return;
    }

    flushList(index);

    const level = headerLevel(line);
    if (level === 0) {
      elements.push(<p key={index}>{line}</p>);
    } else if (level <= 2) {
      elements.push(<h1 key={index}>{headerText(line)}</h1>);
    } else if (level === 3) {
      elements.push(<h2 key={index}>{headerText(line)}</h2>);
    } else {
      elements.push(<h3 key={index}>{headerText(line)}</h3>);
    }
  });

  flushList('end');

  return elements;
};

const isDateHeader = (line) => line.startsWith('* 20') || line.startsWith('## 20');

const TOPIC_HEADER_PATTERN = /^(?:###|\*\*) Topic: (.+)$/;

// takeSection lifts a named section out of a block, returning the rest of the
// block and the section's body.
const takeSection = (block, name, depth) => {
  const prefixes = ['#'.repeat(depth) + ' ', '*'.repeat(depth - 1) + ' '];
  const lines = block.split('\n');
  const start = lines.findIndex((line) => prefixes.some((prefix) => line === prefix + name));
  if (start === -1) {
    return { block, body: '' };
  }

  let end = start + 1;
  while (end < lines.length && headerLevel(lines[end]) === 0) {
    end += 1;
  }

  return {
    block: lines.slice(0, start).concat(lines.slice(end)).join('\n'),
    body: lines.slice(start + 1, end).join('\n').trim(),
  };
};

const parsePhotos = (body) =>
  body
    .split('\n')
    .map((line) => {
      const markdown = line.match(/!\[([^\]]*)\]\(([^)]+)\)/);
      if (markdown) {
        return { caption: markdown[1], src: markdown[2] };
      }
      const org = line.match(/\[\[file:([^\]]+)\]\[([^\]]*)\]\]/);
      if (org) {
        return { caption: org[2], src: org[1] };
      }
      return null;
    })
    .filter(Boolean);

// splitBody pulls the photos and the raw notes out of a block so they can be
// rendered separately from the AI analysis.
const splitBody = (block, depth) => {
  const withoutPhotos = takeSection(block, 'Photos', depth);
  const withoutRaw = takeSection(withoutPhotos.block, 'Raw Input', depth);
  return {
    content: withoutRaw.block.trim(),
    rawInput: withoutRaw.body,
    photos: parsePhotos(withoutPhotos.body),
  };
};

export const parseEntries = (content) => {
  const entries = [];
  let currentEntry = null;
  let currentGroup = null;

  const finishEntry = (entry) => ({
    date: entry.date,
    ...splitBody(entry.content, 3),
    groups: entry.groups.map((group) => ({ topic: group.topic, ...splitBody(group.content, 4) })),
  });

  content.split('\n').forEach((line) => {
    if (isDateHeader(line)) {
      if (currentEntry) {
        entries.push(finishEntry(currentEntry));
      }
      // Remove the leading * or ## from the date
      const date = line.startsWith('* ') ? line.substring(2) : line.substring(3);
      currentEntry = { date, content: '', groups: [] };
      currentGroup = null;
      return;
    }

    if (!currentEntry) {
      return;
    }

    const topicMatch = line.match(TOPIC_HEADER_PATTERN);
    if (topicMatch) {
      currentGroup = { topic: topicMatch[1].trim(), content: '' };
      currentEntry.groups.push(currentGroup);
      return;
    }

    if (currentGroup) {
      currentGroup.content += line + '\n';
    } else {
      currentEntry.content += line + '\n';
    }
  });

  if (currentEntry) {
    entries.push(finishEntry(currentEntry));
  }
  return entries.reverse();
};
