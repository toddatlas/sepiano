<#
.SYNOPSIS
  Saves a full backup of the piano's Pi into a folder on this PC.

.DESCRIPTION
  Downloads everything sepianod keeps (one .zip: pieces, play lists,
  settings, voicing, corrections, the piano's settings backups, programs),
  checks it is a complete sepiano backup, and keeps the newest copies.
  Point -Folder at a folder inside Google Drive (for example
  "G:\My Drive\Piano backups") and the backup goes to the cloud as well.
  See docs/BACKUPS.md. Install-PianoBackupTask.ps1 runs this every few days.

.EXAMPLE
  .\piano-backup.ps1 -Folder "G:\My Drive\Piano backups"
#>
param(
  [string]$Folder = (Join-Path $env:USERPROFILE 'Documents\Piano backups'),
  [string]$Pi = 'http://raspberrypi.local',
  [int]$Keep = 3
)

$ErrorActionPreference = 'Stop'
# Windows PowerShell's download progress display slows Invoke-WebRequest
# many times over.
$ProgressPreference = 'SilentlyContinue'
New-Item -ItemType Directory -Force -Path $Folder | Out-Null
$log = Join-Path $Folder 'backup-log.txt'
function Write-Log([string]$text) {
  $line = '{0:yyyy-MM-dd HH:mm}  {1}' -f (Get-Date), $text
  Add-Content -Path $log -Value $line -Encoding UTF8
  Write-Output $line
}

$name = 'sepiano-backup-{0:yyyyMMdd-HHmm}.zip' -f (Get-Date)
# Download to this PC's own disk first, then move it into the folder: a
# Google Drive folder is slow to write a long download into.
$part = Join-Path ([System.IO.Path]::GetTempPath()) ($name + '.part')
$started = Get-Date
try {
  Invoke-WebRequest -Uri ($Pi.TrimEnd('/') + '/api/v1/backup?by=pc') -OutFile $part -UseBasicParsing -TimeoutSec 3600

  # A complete backup has its manifest (written last).
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $zip = [System.IO.Compression.ZipFile]::OpenRead($part)
  try {
    $manifest = $zip.Entries | Where-Object { $_.FullName -eq 'sepiano/manifest.json' }
    $count = $zip.Entries.Count
  } finally {
    $zip.Dispose()
  }
  if (-not $manifest) { throw 'the download is not a complete sepiano backup' }

  # Copied in under a temporary name, so a half-copied file is never
  # taken for a backup.
  $final = Join-Path $Folder $name
  Move-Item -Path $part -Destination ($final + '.part') -Force
  Move-Item -Path ($final + '.part') -Destination $final -Force
  $size = (Get-Item $final).Length
  Write-Log ('saved {0} ({1:N1} MB, {2} files, {3:N0} s)' -f $name, ($size / 1MB), $count, ((Get-Date) - $started).TotalSeconds)

  # Keep the newest $Keep backups.
  Get-ChildItem -Path $Folder -Filter 'sepiano-backup-*.zip' |
    Sort-Object Name -Descending |
    Select-Object -Skip $Keep |
    ForEach-Object { Remove-Item -LiteralPath $_.FullName; Write-Log ('removed old {0}' -f $_.Name) }
} catch {
  foreach ($p in @($part, (Join-Path $Folder ($name + '.part')))) {
    if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -ErrorAction SilentlyContinue }
  }
  Write-Log ('FAILED: {0}' -f $_.Exception.Message)
  exit 1
}
