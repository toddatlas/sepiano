<#
.SYNOPSIS
  Runs piano-backup.ps1 every few days (Windows Task Scheduler).

.DESCRIPTION
  Copies piano-backup.ps1 to %USERPROFILE%\sepiano and registers a task,
  "Piano backup", that runs it every -EveryDays days (3) at -At, keeping the
  newest -Keep (3), for the current user. If the PC
  is off or asleep then, it runs as soon as it can. Run it again to change
  the folder, the interval or the time. To stop the backups:
  Unregister-ScheduledTask -TaskName 'Piano backup'
  See docs/BACKUPS.md.

.EXAMPLE
  .\Install-PianoBackupTask.ps1 -Folder "G:\My Drive\Piano backups" -At 3am
#>
param(
  [Parameter(Mandatory = $true)][string]$Folder,
  [string]$Pi = 'http://raspberrypi.local',
  [datetime]$At = '3:00',
  [int]$EveryDays = 3,
  [int]$Keep = 3
)

$ErrorActionPreference = 'Stop'
# Not under AppData: a packaged app (the Claude desktop app, for one) writes
# there into a private copy that Task Scheduler cannot see.
$home_ = Join-Path $env:USERPROFILE 'sepiano'
New-Item -ItemType Directory -Force -Path $home_ | Out-Null
$script = Join-Path $home_ 'piano-backup.ps1'
Copy-Item -Path (Join-Path $PSScriptRoot 'piano-backup.ps1') -Destination $script -Force

$args_ = '-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "{0}" -Folder "{1}" -Pi "{2}" -Keep {3}' -f $script, $Folder, $Pi, $Keep
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $args_
$trigger = New-ScheduledTaskTrigger -Daily -DaysInterval $EveryDays -At $At
# A laptop on battery would hold the task until plugged in (Windows' default);
# a backup takes some minutes, so it runs on battery too.
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -RunOnlyIfNetworkAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Hours 2)
Register-ScheduledTask -TaskName 'Piano backup' -Action $action -Trigger $trigger -Settings $settings `
  -Description 'Saves a full backup of the piano''s Pi (sepiano) into a folder; see docs/BACKUPS.md' -Force | Out-Null

Write-Output ('"Piano backup" will run every {0} day(s) at {1:t}, into {2}, keeping {3}.' -f $EveryDays, $At, $Folder, $Keep)
Write-Output 'To try it now:  Start-ScheduledTask -TaskName "Piano backup"'
