# AI Journal

A web-based journaling application that uses Google's Gemini AI to parse unstructured thoughts into structured journal entries. It features a secure login, a Gruvbox-themed UI, and stores entries in a private GitHub repository, a Postgres database such as [Supabase](https://supabase.com), or both. Supports both Markdown and Org-mode output formats.

## Features

-   **AI Analysis**: Uses Gemini 2.5 Flash to break down entries into:
    -   Emotional Check-in
    -   Things that made you happy
    -   Things that were stressful
    -   Focus items for next time
-   **Topics**: Group a day's entries under a topic (a conference talk, a meeting) so many short notes collect in one place. The AI summary is then re-synthesized from *all* of that topic's notes, instead of each note being summarized on its own.
-   **Photos**: Attach a picture straight from the phone camera. It is saved alongside your entries and linked from the entry (or from the topic, when one is set).
-   **Git Storage**: Automatically commits and pushes entries to a specified GitHub repository in Markdown or Org-mode format.
-   **SQL Storage**: Keeps entries in Postgres (e.g. Supabase's free tier) instead of, or as well as, the git repo. The schema is created and migrated automatically at startup.
-   **Secure Access**: Simple password-based authentication with session management.
-   **Beautiful UI**: A responsive React frontend styled with the Gruvbox Dark theme.

## Prerequisites

-   **Go**: 1.25 or later
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
| `GITHUB_TOKEN` | A GitHub personal access token that can push to that repository. | Yes (for sync) |
| `DATABASE_URL` | A Postgres connection string. When set, entries are stored in the database. See [SQL Storage](#sql-storage-supabase). | No |

### Where entries are stored

| `DATABASE_URL` | `GIT_USERNAME`, `GIT_REPO_NAME` and `GITHUB_TOKEN` | Entries are stored in |
| :--- | :--- | :--- |
| Not set | Set | The git storage repo |
| Set | Not set | Postgres |
| Set | Set | Postgres, mirrored to the git storage repo |
| Not set | Not set | `journal.md` and `notes.md` in the working directory |

With both configured, the app reads from Postgres and writes every entry, analysis and photo to both, so the Markdown or Org files in git keep growing as before. A write that fails on the git side is logged but doesn't fail the request, since the entry is already safe in Postgres.

Entries already in the git repo are not copied into the database, so **Past Entries** starts from the day you add `DATABASE_URL`.

### Setting up the Storage Repo

1.  Create a private repository on GitHub (e.g., `journal-entries`).
2.  Ensure your local machine has SSH keys configured for your GitHub account.
3.  The application will automatically clone this repo into a `journal_storage` directory on first run.
4.  Photo attachments are written to `images/<date>/` inside that repo and pushed along with the notes, so keep an eye on its size if you attach a lot of them.

### SQL Storage (Supabase)

Set `DATABASE_URL` to a Postgres connection string and entries are stored in the database. Any Postgres database works; Supabase's free tier is plenty for a journal.

1.  Create a project on [Supabase](https://supabase.com) and keep the database password you choose.
2.  Click **Connect** on the project dashboard and copy the **Session pooler** connection string. It looks like `postgresql://postgres.<project-ref>:[YOUR-PASSWORD]@aws-0-<region>.pooler.supabase.com:5432/postgres`.
3.  Put your password in place of `[YOUR-PASSWORD]` (percent-encode any special characters in it), and add `?sslmode=require` to the end so the connection is always encrypted.
4.  Set the result as `DATABASE_URL`, e.g. `fly secrets set DATABASE_URL='postgresql://...'` on Fly.io.

The session pooler works over IPv4 and IPv6. Supabase's direct connection (`db.<project-ref>.supabase.co`) is IPv6-only unless you add their IPv4 add-on. If you use the transaction pooler (port `6543`) instead, also add `default_query_exec_mode=simple_protocol` to the connection string, since that pooler can't keep prepared statements.

Supabase pauses free-plan projects after a period of inactivity. If the app logs that it can't reach the database, check whether the project needs restoring from the dashboard.

#### Migrations

There's nothing to set up by hand. On startup the app applies any migrations in [`migrations/`](migrations) that the database hasn't run yet, creating the tables on first start. If the database can't be reached at startup (the network isn't up yet as the machine boots, or the project is paused), the app starts anyway and retries the migrations on the next request.

To change the schema, add a file to `migrations/` with the next number, e.g. `0002_add_mood.sql`. Pending migrations run together in one transaction and are recorded in `journal.schema_migrations`. Never edit a migration that has already run, since databases that have applied it won't apply it again.

#### Schema

Everything lives in a `journal` schema:

| Table | Holds |
| :--- | :--- |
| `journal.entries` | Every note exactly as typed (`raw_input`), with the AI `analysis` (JSON) for notes without a topic. |
| `journal.topics` | One row per topic per day, with the AI `synthesis` (JSON) of all of its notes. |
| `journal.photos` | Photo attachments: the image bytes, caption, and the path they're served at. |
| `journal.schema_migrations` | The migrations that have been applied. |

Supabase exposes the `public` schema through its auto-generated REST API; the `journal` schema is not exposed. Row level security is also enabled on every table with no policies, so only the database role the app connects as can read your journal.

Past Entries is rendered from these tables into the same Markdown or Org layout the git repo uses, so the UI works the same way with either store. Changing `JOURNAL_FORMAT` changes how every entry is shown, old ones included.

With Postgres, photos are stored in the database too. The browser downscales them before upload, so each is typically a few hundred KB, but keep an eye on your database size if you attach a lot of them: Supabase's free plan has a 500 MB database limit at the time of writing.

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
export DATABASE_URL="postgres://..."  # Optional: store entries in Postgres

# Start the app
make dev
```

-   **Frontend**: http://localhost:3000
-   **Backend**: http://localhost:8080

### Tests

```bash
go test ./...
```

The Postgres tests are skipped unless `TEST_DATABASE_URL` points at a server where they may create and drop scratch databases:

```bash
TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" go test ./...
```

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
    -   Save your text straight away, before any AI processing.
    -   Send the text to Gemini for analysis.
    -   Format the response into a structured entry (Markdown by default, or Org-mode if `JOURNAL_FORMAT=org` is set).
    -   With git storage, append it to `journal.md` (or `journal.org` if using Org-mode) in your Git repo, and commit and push the changes to GitHub.
    -   With `DATABASE_URL` set, save the entry and its analysis to Postgres.

### Topics

Set a **Topic** on the New Entry screen to group everything that follows under one heading for the day — a conference talk, a meeting, a chapter of a book. Topics already used today appear as chips, so a talk can be resumed with a tap, and the current topic survives a page reload. Leave the topic empty and entries are filed directly under the day, exactly as before.

What a topic changes:

-   Every note (and photo) posted with that topic lands in the same topic block, rather than being appended as another entry for the day.
-   When a new note arrives, the AI is given **all** of that topic's notes so far and asked to synthesize them as one session. The resulting summary **replaces** the topic's previous one — it doesn't stack up. A long talk therefore ends with a single coherent summary rather than a dozen fragments.
-   The raw notes and photos are never rewritten; only the analysis sections are.

Topics are just headings in the file, so notes taken under a topic stay readable and greppable in plain Markdown or Org.

### Photos

Tap **📷 Add Photo** to open the phone camera (or the file picker on a desktop). The capture is downscaled in the browser to at most 1600px on its longest edge, then uploaded, stored (committed to `images/<date>/` inside the storage repo, or saved in the database with `DATABASE_URL` set), and linked from the current entry — under the current topic if one is set.

Photos are served back to the UI through `/api/media/...`, which requires a logged-in session and only serves photos under `images/`.

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

I've just been deploying manually on fly.io with their UI. To store entries in Supabase there, set the connection string as a secret (see [SQL Storage](#sql-storage-supabase)):

```bash
fly secrets set DATABASE_URL='postgresql://postgres.<project-ref>:<password>@aws-0-<region>.pooler.supabase.com:5432/postgres?sslmode=require'
```