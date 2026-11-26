# Backups

Everything the piano service keeps lives on the Pi's memory card: about
48 MB on 2026-11-26. That includes the only copies of the recordings made
on this piano. Memory cards fail, so keep backups on a computer and, best,
in the cloud.

## What a backup holds

One `.zip` file, `sepiano-backup-YYYYMMDD-HHMM.zip`, with the Pi's whole
data folder (`/var/lib/sepiano`) under `sepiano/`:

| In the backup | What it is |
|---------------|------------|
| `library/` | every piece: `.kb`, `.lp`, `.sp`, its details (`.json`: title, album, hidden, grade …), and the original `.mid` of a converted MIDI file |
| `playlists.json` | the play lists |
| `settings.json` | preferred instrument, level, volume method |
| `voicing.json`, `corrections.json` | software voicing and the recording corrections |
| `eeprom/` | the piano's settings backups (its calibration) |
| `programs/` | the installed suite programs: the owner's own copies |
| `staging/` | a recording not yet saved, if any |
| `manifest.json` | the list of files, each with its size and SHA-256 checksum, and when the backup was made |

Left out: `play/`, the joined albums, which are made again when played;
scratch folders (`work/`); and temporary files.

The programs and the recordings are copyrighted. A backup is for the
owner's own safekeeping. Never put one anywhere public, the repository
included.

## Backing up by hand

Status → **Backups** → **Back up everything**. The browser saves the file.
To choose where it goes, set the browser to ask where to save downloads
(Chrome and Edge: Settings → Downloads → "Ask where to save each file").
Pick a folder inside **Google Drive** (on the owner's PC, Google Drive for
desktop is drive `G:`, so for example `G:\My Drive\Piano backups`). Google
Drive then copies it to the cloud.

The card shows when the last full backup was made and how. It warns when
there is none, or when the last is a week old or more.

## Automatic backups from the PC, every 3 days

**Set up on the owner's PC on 2026-11-26:** every 3 days at 3 a.m., into
`G:\My Drive\Piano backups` (Google Drive), keeping the newest 3. The
owner chose this (2026-11-26): the library changes slowly, so a backup
every 3 days and three copies are enough. It was nightly, keeping 14, for
the first day. The first runs saved 24.6 MB each; after the content
libraries were imported a backup is about 150 MB.

`deploy/windows/piano-backup.ps1` downloads a backup into a folder:
- it checks the download is complete (it has its manifest);
- it keeps the newest 3 (`-Keep`), removing older ones only after a new
  one is saved;
- it notes each run in `backup-log.txt` in that folder.

`deploy/windows/Install-PianoBackupTask.ps1` makes Windows run it every
3 days. In PowerShell, from the repository's `deploy\windows` folder:

```powershell
.\Install-PianoBackupTask.ps1 -Folder "G:\My Drive\Piano backups" -At 3am
```

`-EveryDays` (3) and `-Keep` (3) change the interval and how many are
kept.

- The task is called **"Piano backup"**, and it runs as the signed-in
  user.
- Its script is kept in `%USERPROFILE%\sepiano` (for example
  `C:\Users\todd\sepiano`), not under AppData. A packaged app such as the
  Claude desktop app writes AppData into a private copy that Task
  Scheduler cannot see, and that stopped the first try (2026-11-26).
- It runs on battery too: Windows would otherwise hold it until a laptop
  is plugged in.
- If the PC is off or asleep at 3 a.m., it runs as soon as it can.
- To try it now: `Start-ScheduledTask -TaskName "Piano backup"`, then
  look in the folder and its `backup-log.txt`.
- To stop the automatic backup: `Unregister-ScheduledTask -TaskName "Piano
  backup"`.
- The Pi records each backup the PC takes, so Status → Backups shows "by
  the PC's automatic backup".

The script uses `http://raspberrypi.local`. If the PC cannot find the Pi
by that name, add `-Pi http://192.168.6.164` (the Pi's address on the home
network today; it can change).

## Restoring

Status → Backups → **Restore from a backup…**, then choose the `.zip`.
Restoring **adds** what the backup has and this Pi lacks. **Nothing on
the Pi is changed or removed:**
- pieces: those not here are put back with their details. A piece already
  here (the same performance) is left as it is. If another piece has
  taken a restored piece's id, it gets a new one;
- play lists not here are added;
- the piano's settings backups not here are added. Restoring one *to the
  piano* is a separate step (Piano → Tools → Restore a backup);
- programs not installed are installed, after the usual identity check;
- `settings.json`, `voicing.json` and `corrections.json` are put back only
  if this Pi has none (a fresh Pi);
- a recording in `staging/` is not restored.

Every file is checked against the manifest before it is used. A report
says what was added, what was already there, and any problem.

**Rebuilding the Pi from nothing:**
1. Install a fresh system and `sepianod` (`deploy/deploy.sh`).
2. Open the app.
3. Restore the newest backup.

The library, play lists, settings, voicing, corrections, the piano's
settings backups and the programs all come back.

## For later

- From the Pi straight to Google Drive (`rclone`, after a one-time
  sign-in), so backups happen even when the PC is off.
- Backups that send only what changed. Not needed yet: a whole backup
  takes a few seconds.

## API

- `GET /api/v1/backup`: the zip. `?by=pc` marks it as taken by the PC
  script.
- `GET /api/v1/backup/status`: when the last full backup was made, by
  what, how many pieces, and the size of the data now.
- `POST /api/v1/backup/restore`: the zip, as the body or a multipart
  `file`. Returns the report.

Code: `backup/` (writing and checking the zip), `api/backup.go` (download,
status, restore), `library/restore.go` (pieces and play lists).
