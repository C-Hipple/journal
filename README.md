# AI Journal

A web-based journaling application that uses Google's Gemini AI to parse unstructured thoughts into structured journal entries. It features a secure login, a Gruvbox-themed UI, and automatic synchronization with a private GitHub repository. Supports both Markdown and Org-mode output formats.

## Features

-   **AI Analysis**: Uses Gemini 2.5 Flash to break down entries into:
    -   Emotional Check-in
    -   Things that made you happy
    -   Things that were stressful
    -   Focus items for next time
-   **Topics**: Group a day's entries under a topic (a conference talk, a meeting) so many short notes collect in one place. The AI summary is then re-synthesized from *all* of that topic's notes, instead of each note being summarized on its own.
-   **Photos**: Attach a picture straight from the phone camera. It is saved into the storage repo and linked from the entry (or from the topic, when one is set).
-   **Git Storage**: Automatically commits and pushes entries to a specified GitHub repository in Markdown or Org-mode format.
-   **Secure Access**: Simple password-based authentication with session management.
-   **Beautiful UI**: A responsive React frontend styled with the Gruvbox Dark theme.

## Prerequisites

-   **Go**: 1.18 or later
-   **Node.js**: 16 or later
-   **Git**: Configured with SSH access to GitHub.

## Configuration

The application is configured via environment variables. You must set these before running the app:

| Variable | Description | Required |
| :--- | :--- | :--- |
| `JOURNAL_PASSWORD` | The password required to log in to the web interface. | Yes |
| `GEMINI_API_TOKEN` | Your Google Gemini API key for AI processing. | Yes |
| `JOURNAL_FORMAT` | Output format for journal entries. Must be `"org"` or `"markdown"`. Defaults to `"markdown"`. | No |
| `GIT_USERNAME` | Your GitHub username (e.g., `chris`). | Yes (for sync) |
| `GIT_REPO_NAME` | The name of the private repository to store entries (e.g., `journal-entries`). | Yes (for sync) |

### Setting up the Storage Repo

1.  Create a private repository on GitHub (e.g., `journal-entries`).
2.  Ensure your local machine has SSH keys configured for your GitHub account.
3.  The application will automatically clone this repo into a `journal_storage` directory on first run.
4.  Photo attachments are written to `images/<date>/` inside that repo and pushed along with the notes, so keep an eye on its size if you attach a lot of them.

## Running the Application

### Development Mode

To run with hot-reloading for the frontend and the Go backend:

```bash
# Set your env vars
export JOURNAL_PASSWORD="mysecretpassword"
export GEMINI_API_TOKEN="your_gemini_key"
export JOURNAL_FORMAT="markdown"  # Optional: "org" or "markdown" (default: "markdown")
export GIT_USERNAME="your_github_user"
export GIT_REPO_NAME="your_repo_name"  # This is where your notes are stored, NOT THIS REPO!!!

# Start the app
make dev
```

-   **Frontend**: http://localhost:3000
-   **Backend**: http://localhost:8080

### Production Build

To build a single binary that serves the frontend statically:

```bash
make build
make run
```

The application will be available at http://localhost:8080.

## Usage

1.  Open the app in your browser.
2.  Log in with your `JOURNAL_PASSWORD`.
3.  Type your raw thoughts into the text area and click **Save Entry**.
4.  The app will:
    -   Send the text to Gemini for analysis.
    -   Format the response into a structured entry (Markdown by default, or Org-mode if `JOURNAL_FORMAT=org` is set).
    -   Append it to `journal.md` (or `journal.org` if using Org-mode) in your Git repo.
    -   Commit and push the changes to GitHub.

### Topics

Set a **Topic** on the New Entry screen to group everything that follows under one heading for the day — a conference talk, a meeting, a chapter of a book. Topics already used today appear as chips, so a talk can be resumed with a tap, and the current topic is remembered so it only has to be typed once. Leave the topic empty and entries are filed directly under the day, exactly as before.

What a topic changes:

-   Every note (and photo) posted with that topic lands in the same topic block, rather than being appended as another entry for the day.
-   When a new note arrives, the AI is given **all** of that topic's notes so far and asked to synthesize them as one session. The resulting summary **replaces** the topic's previous one — it doesn't stack up. A long talk therefore ends with a single coherent summary rather than a dozen fragments.
-   The raw notes and photos are never rewritten; only the analysis sections are.

The current topic is kept in a `journal_topic` cookie that expires at midnight, so it survives a reload, a locked phone or a second tab — the talk's name is typed once and every note that follows joins it — while yesterday's talk is never silently attached to this morning's notes. Clearing the topic clears the cookie.

Topics are just headings in the file, so notes taken under a topic stay readable and greppable in plain Markdown or Org.

### Photos

Tap **📷 Add Photo** to open the phone camera (or the file picker on a desktop). The capture is downscaled in the browser to at most 1600px on its longest edge, then uploaded, written to `images/<date>/` inside the storage repo, committed, and linked from the current entry — under the current topic if one is set.

Photos are served back to the UI through `/api/media/...`, which requires a logged-in session and only serves files under `images/`.

## Output Format

Entries are saved in `journal.md` (default) or `journal.org` (if `JOURNAL_FORMAT=org` is set). The format can be controlled via the `JOURNAL_FORMAT` environment variable.

### Markdown Format (Default)

```markdown
## 2025-01-15 Mon

### General Emotional Checkin

Feeling productive but slightly tired.

### Things that made me happy

- Coding a new feature
- Coffee

### Things that were stressful

- Debugging a race condition

### Things I want to focus on doing for next time

- Take more breaks

### Raw Input

Today I worked on a new feature...
```

With a topic set, the day's entry nests one level deeper:

```markdown
## 2025-01-15 Mon

### Topic: Scaling Postgres to 100TB

#### Summary

A walkthrough of moving from a single primary to tenant-based sharding.

#### Notes

- Shard key is tenant id
- WAL shipping is async by default

#### Photos

![Scaling Postgres to 100TB 09:12](images/2025-01-15/091200-scaling-postgres.jpg)

#### Raw Input

they shard by tenant id
wal shipping is async by default
```

### Org-mode Format

Set `JOURNAL_FORMAT=org` to use Org-mode format:

```org
* 2025-01-15 Mon
** General Emotional Checkin
Feeling productive but slightly tired.
** Things that made me happy
- Coding a new feature
- Coffee
** Things that were stressful
- Debugging a race condition
** Things I want to focus on doing for next time
- Take more breaks
** Raw Input
Today I worked on a new feature...
```

Topics nest the same way in Org-mode, and photos become file links:

```org
* 2025-01-15 Mon
** Topic: Scaling Postgres to 100TB
*** Summary
A walkthrough of moving from a single primary to tenant-based sharding.
*** Notes
- Shard key is tenant id
*** Photos
[[file:images/2025-01-15/091200-scaling-postgres.jpg][Scaling Postgres to 100TB 09:12]]
*** Raw Input
they shard by tenant id
```

## Deployment

I've just been deploying manually on fly.io with their UI. 