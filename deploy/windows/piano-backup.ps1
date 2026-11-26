<#
.SYNOPSIS
  Saves a full backup of the piano's Pi into a folder on this PC.

.DESCRIPTION
  Downloads everything sepianod keeps (one .zip: pieces, play lists,
  settings, voicing, corrections, the piano's settings backups, programs),
  checks it is a complete sepiano backup, and keeps the newest copies.
  Point -Folder at a folder inside Google Drive (for example
  "G:\My Drive\Piano backups") and the backup goes to the cloud as well.
  See docs/BACKUPS.md. Install-PianoBackupTask.ps1 runs this every night.

.EXAMPLE
  .\piano-backup.ps1 -Folder "G:\My Drive\Piano backups"
#>
param(
  [string]$Folder = (Join-Path $env:USERPROFILE 'Documents\Piano backups'),
  [string]$Pi = 'http://raspberrypi.local',
  [int]$Keep = 14
)

$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path $Folder | Out-Null
$log = Join-Path $Folder 'backup-log.txt'
function Write-Log([string]$text) {
  $line = '{0:yyyy-MM-dd HH:mm}  {1}' -f (Get-Date), $text
  Add-Content -Path $log -Value $line -Encoding UTF8
  Write-Output $line
}

$name = 'sepiano-backup-{0:yyyyMMdd-HHmm}.zip' -f (Get-Date)
$part = Join-Path $Folder ($name + '.part')
try {
  Invoke-WebRequest -Uri ($Pi.TrimEnd('/') + '/api/v1/backup?by=pc') -OutFile $part -UseBasicParsing -TimeoutSec 900

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

  Move-Item -Path $part -Destination (Join-Path $Folder $name) -Force
  $size = (Get-Item (Join-Path $Folder $name)).Length
  Write-Log ('saved {0} ({1:N1} MB, {2} files)' -f $name, ($size / 1MB), $count)

  # Keep the newest $Keep backups.
  Get-ChildItem -Path $Folder -Filter 'sepiano-backup-*.zip' |
    Sort-Object Name -Descending |
    Select-Object -Skip $Keep |
    ForEach-Object { Remove-Item -LiteralPath $_.FullName; Write-Log ('removed old {0}' -f $_.Name) }
} catch {
  if (Test-Path $part) { Remove-Item -LiteralPath $part -ErrorAction SilentlyContinue }
  Write-Log ('FAILED: {0}' -f $_.Exception.Message)
  exit 1
}
