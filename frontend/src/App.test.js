import { endOfToday, parseEntries, readStoredTopic, writeStoredTopic } from './App';

const MARKDOWN = `## 2026-09-15 Tue

### Summary

arrived at the conference

### Raw Input

conference wifi is flaky

### Topic: Scaling Postgres

#### Summary

sharding and replication

#### Notes

- shard by tenant
- wal shipping is async

#### Photos

![Scaling Postgres 09:00](images/2026-09-15/090000-scaling-postgres.png)

#### Raw Input

they shard by tenant id
wal shipping is async by default

### Topic: Rust in Production

#### Raw Input

borrow checker war stories

## 2026-09-16 Wed

### Raw Input

day two
`;

const ORG = `* 2026-09-15 Tue
** Topic: Keynote
*** Summary
the opening talk
*** Photos
[[file:images/2026-09-15/090000-keynote.png][Keynote 09:00]]
*** Raw Input
first note
second note
`;

test('groups a day\'s notes under their topics', () => {
  const entries = parseEntries(MARKDOWN);

  // Newest day first.
  expect(entries.map((e) => e.date)).toEqual(['2026-09-16 Wed', '2026-09-15 Tue']);

  const conferenceDay = entries[1];
  expect(conferenceDay.groups.map((g) => g.topic)).toEqual([
    'Scaling Postgres',
    'Rust in Production',
  ]);

  // Ungrouped sections stay on the day itself.
  expect(conferenceDay.rawInput).toBe('conference wifi is flaky');
  expect(conferenceDay.content).toContain('arrived at the conference');
  expect(conferenceDay.content).not.toContain('sharding and replication');

  const postgres = conferenceDay.groups[0];
  expect(postgres.content).toContain('sharding and replication');
  expect(postgres.content).toContain('- shard by tenant');
  expect(postgres.rawInput).toBe('they shard by tenant id\nwal shipping is async by default');

  // Notes must not leak between topics.
  expect(conferenceDay.groups[1].rawInput).toBe('borrow checker war stories');
});

test('extracts photo links from a topic', () => {
  const postgres = parseEntries(MARKDOWN)[1].groups[0];

  expect(postgres.photos).toEqual([
    { caption: 'Scaling Postgres 09:00', src: 'images/2026-09-15/090000-scaling-postgres.png' },
  ]);
  // The photo link belongs in the gallery, not in the rendered prose.
  expect(postgres.content).not.toContain('images/2026-09-15');
});

test('parses org mode topics and photos', () => {
  const [day] = parseEntries(ORG);

  expect(day.date).toBe('2026-09-15 Tue');
  expect(day.groups).toHaveLength(1);

  const keynote = day.groups[0];
  expect(keynote.topic).toBe('Keynote');
  expect(keynote.content).toContain('the opening talk');
  expect(keynote.rawInput).toBe('first note\nsecond note');
  expect(keynote.photos).toEqual([
    { caption: 'Keynote 09:00', src: 'images/2026-09-15/090000-keynote.png' },
  ]);
});

test('handles a day with no topics', () => {
  const entries = parseEntries('## 2026-09-14 Mon\n\n### Raw Input\n\njust a thought\n');

  expect(entries).toHaveLength(1);
  expect(entries[0].groups).toEqual([]);
  expect(entries[0].rawInput).toBe('just a thought');
  expect(entries[0].photos).toEqual([]);
});

describe('the daily topic cookie', () => {
  beforeEach(() => {
    document.cookie = 'journal_topic=; Max-Age=0; Path=/';
  });

  test('a topic set once is still there on the next page load', () => {
    writeStoredTopic('Scaling Postgres');

    expect(readStoredTopic()).toBe('Scaling Postgres');
  });

  test('survives characters a cookie cannot carry literally', () => {
    writeStoredTopic('Rust, in production; part 2 = fun');

    expect(document.cookie).not.toContain('production;');
    expect(readStoredTopic()).toBe('Rust, in production; part 2 = fun');
  });

  test('clearing the topic clears the cookie', () => {
    writeStoredTopic('Keynote');
    writeStoredTopic('');

    expect(readStoredTopic()).toBe('');
    expect(document.cookie).not.toContain('journal_topic=');
  });

  test('no cookie means no topic, not a crash', () => {
    expect(readStoredTopic()).toBe('');
  });

  test('a malformed cookie reads as no topic', () => {
    document.cookie = 'journal_topic=%E0%A4%A; Path=/';

    expect(readStoredTopic()).toBe('');
  });

  test("the topic is the day's, so it expires tonight", () => {
    const now = new Date();
    const midnight = endOfToday();

    expect(midnight.getTime()).toBeGreaterThan(now.getTime());
    expect(midnight.getDate()).toBe(now.getDate());
    expect([midnight.getHours(), midnight.getMinutes()]).toEqual([23, 59]);
  });
});
