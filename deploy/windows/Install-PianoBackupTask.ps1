<#
.SYNOPSIS
  Runs piano-backup.ps1 every night (Windows Task Scheduler).

.DESCRIPTION
  Copies piano-backup.ps1 to %LOCALAPPDATA%\sepiano and registers a task,
  "Piano backup", that runs it daily at -At for the current user. If the PC
  is off or asleep then, it runs as soon as it can. Run it again to change
  the folder or the time. To stop the nightly backup:
  Unregister-ScheduledTask -TaskName 'Piano backup'
  See docs/BACKUPS.md.

.EXAMPLE
  .\Install-PianoBackupTask.ps1 -Folder "G:\My Drive\Piano backups" -At 3am
#>
param(
  [Parameter(Mandatory = $true)][string]$Folder,
  [string]$Pi = 'http://raspberrypi.local',
  [datetime]$At = '3:00',
  [int]$Keep = 14
)

$ErrorActionPreference = 'Stop'
$home_ = Join-Path $env:LOCALAPPDATA 'sepiano'
New-Item -ItemType Directory -Force -Path $home_ | Out-Null
$script = Join-Path $home_ 'piano-backup.ps1'
Copy-Item -Path (Join-Path $PSScriptRoot 'piano-backup.ps1') -Destination $script -Force

$args_ = '-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "{0}" -Folder "{1}" -Pi "{2}" -Keep {3}' -f $script, $Folder, $Pi, $Keep
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $args_
$trigger = New-ScheduledTaskTrigger -Daily -At $At
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -RunOnlyIfNetworkAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 30)
Register-ScheduledTask -TaskName 'Piano backup' -Action $action -Trigger $trigger -Settings $settings `
  -Description 'Saves a full backup of the piano''s Pi (sepiano) into a folder; see docs/BACKUPS.md' -Force | Out-Null

Write-Output ('"Piano backup" will run every day at {0:t}, into {1}.' -f $At, $Folder)
Write-Output 'To try it now:  Start-ScheduledTask -TaskName "Piano backup"'
