param(
    [string]$ConfigPath = (Join-Path $PSScriptRoot 'send-mail.config.yml'),
    [string]$User,
    [string]$AppPassword,
    [string]$To,
    [string]$Day,
    [string]$LogDir,
    [string]$Subject,
    [string]$Body,
    [string]$SmtpHost,
    [int]$SmtpPort,
    [switch]$RegisterTask,
    [string]$TaskName
)

# Ensure console uses UTF-8 to avoid garbled text
try { chcp 65001 | Out-Null } catch {}
$utf8 = [System.Text.UTF8Encoding]::new()
[Console]::OutputEncoding = $utf8
[Console]::InputEncoding  = $utf8
$OutputEncoding           = $utf8

function Ensure-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($id)
    if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { return }

    Write-Host "Non-admin detected. Relaunching as Administrator..." -ForegroundColor Yellow

    $argsList = @('-ExecutionPolicy','Bypass','-File',$PSCommandPath)
    function AddArg {
        param($name,$value)
        if ($null -ne $value -and $value -ne '') {
            $argsList += "-$name"
            $argsList += [string]$value
        }
    }

    AddArg 'ConfigPath' $ConfigPath
    AddArg 'User' $User
    AddArg 'AppPassword' $AppPassword
    AddArg 'To' $To
    AddArg 'Day' $Day
    AddArg 'LogDir' $LogDir
    AddArg 'Subject' $Subject
    AddArg 'Body' $Body
    AddArg 'SmtpHost' $SmtpHost
    AddArg 'SmtpPort' $SmtpPort
    if ($RegisterTask) { $argsList += '-RegisterTask' }
    AddArg 'TaskName' $TaskName

    Start-Process -FilePath (Get-Command powershell).Source -ArgumentList $argsList -Verb RunAs | Out-Null
    exit
}

Ensure-Admin

function Load-ConfigFile {
    param([string]$Path)
    if (-not $Path) { return @{} }
    if (-not (Test-Path -LiteralPath $Path)) { return @{} }

    $ext = [IO.Path]::GetExtension($Path).ToLowerInvariant()
    $content = Get-Content -LiteralPath $Path -Raw

    if ($ext -eq '.yml' -or $ext -eq '.yaml') {
        if (Get-Command ConvertFrom-Yaml -ErrorAction SilentlyContinue) {
            return (ConvertFrom-Yaml -Yaml $content)
        }

        # fallback: simple key:value parsing without warning to keep output clean
        $yamlResult = @{}
        foreach ($line in ($content -split "`n")) {
            $trim = $line.Trim()
            if (-not $trim -or $trim.StartsWith('#')) { continue }
            $colonIndex = $trim.IndexOf(':')
            if ($colonIndex -gt 0) {
                $k = $trim.Substring(0, $colonIndex).Trim()
                $v = $trim.Substring($colonIndex + 1).Trim()
                if ($k) { $yamlResult[$k] = $v }
            }
        }
        return $yamlResult
    }

    $result = @{}
    foreach ($line in ($content -split "`n")) {
        $trim = $line.Trim()
        if (-not $trim -or $trim.StartsWith('#')) { continue }
        $pair = $trim.Split('=', 2)
        if ($pair.Count -eq 2) {
            $result[$pair[0].Trim()] = $pair[1].Trim()
        }
    }
    return $result
}

function Resolve-Setting {
    param(
        [string]$ParamValue,
        [string]$EnvValue,
        [hashtable]$Config,
        [string]$Key,
        [string]$Default,
        [switch]$Required
    )

    $fromConfig = $null
    if ($Config -and $Config.ContainsKey($Key)) {
        $fromConfig = [string]$Config[$Key]
    }

    $value = $ParamValue
    if (-not $value) { $value = $EnvValue }
    if (-not $value) { $value = $fromConfig }
    if (-not $value) { $value = $Default }

    if ($Required -and (-not $value)) {
        throw "missing required setting: $Key"
    }

    return $value
}

function Prompt-Input {
    param(
        [string]$Message,
        [switch]$Secure
    )

    Write-Host $Message -NoNewline
    if ($Secure) {
        $sec = Read-Host -AsSecureString
        return [Runtime.InteropServices.Marshal]::PtrToStringUni([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
    }
    return (Read-Host)
}

function Ensure-Setting {
    param(
        [string]$Value,
        [string]$Prompt,
        [switch]$Secure
    )

    if ($Value) { return $Value }
    return (Prompt-Input -Message $Prompt -Secure:$Secure)
}

$cfg = Load-ConfigFile -Path $ConfigPath

$EnvHost = if ($Env:XWATCH_SMTP_HOST) { $Env:XWATCH_SMTP_HOST } else { $Env:SMTP_HOST }
$EnvPort = if ($Env:XWATCH_SMTP_PORT) { $Env:XWATCH_SMTP_PORT } else { $Env:SMTP_PORT }
$EnvUser = if ($Env:XWATCH_SMTP_USER) { $Env:XWATCH_SMTP_USER } else { $Env:SMTP_USER }
$EnvPass = if ($Env:XWATCH_SMTP_PASS) { $Env:XWATCH_SMTP_PASS } else { $Env:SMTP_PASS }

$SmtpHost     = Resolve-Setting $SmtpHost     $EnvHost $cfg 'host' 'mail.httc.com.tw'
$SmtpPortText = Resolve-Setting ([string]$SmtpPort) $EnvPort $cfg 'port' '587'
if (-not [int]::TryParse($SmtpPortText, [ref]$SmtpPort)) { $SmtpPort = 587 }
if (-not $SmtpPort -or $SmtpPort -le 0) { $SmtpPort = 587 }

$User        = Resolve-Setting $User        $EnvUser $cfg 'user'        $null
$AppPassword = Resolve-Setting $AppPassword $EnvPass $cfg 'appPassword' $null
$To          = Resolve-Setting $To          $Env:XWATCH_MAIL_TO   $cfg 'to'          'r021@httc.com.tw'
$Day         = Resolve-Setting $Day         $null                 $cfg 'day'         (Get-Date).AddDays(-1).ToString('yyyy-MM-dd')
$LogDir      = Resolve-Setting $LogDir      $Env:XWATCH_LOG_DIR   $cfg 'logDir'      (Join-Path $Env:ProgramData 'go-xwatch\xwatch-watch-logs')
$Subject     = Resolve-Setting $Subject     $null                 $cfg 'subject'     $null
$Body        = Resolve-Setting $Body        $null                 $cfg 'body'        $null
$TaskName    = Resolve-Setting $TaskName    $null                 $cfg 'taskName'    'XWatchMailDaily'

$User        = Ensure-Setting $User "Enter SMTP user:" 
$AppPassword = Ensure-Setting $AppPassword "Enter SMTP app password:" -Secure
$To          = Ensure-Setting $To "Enter recipient(s) (comma separated):"

# 確保日誌目錄存在；若不存在或權限不足，直接提示以系統管理員重新執行（前面 Ensure-Admin 已自動提權）
try {
    if (-not (Test-Path -LiteralPath $LogDir -ErrorAction Stop)) {
        throw ("log directory not found: {0}" -f $LogDir)
    }
} catch {
    throw ("log directory not accessible: {0} (try running as Administrator)" -f $LogDir)
}

function Get-LatestLogFile {
    param([string]$Dir)
    return Get-ChildItem -LiteralPath $Dir -Filter 'watch*.log' -File -ErrorAction SilentlyContinue |
        Where-Object { $_.Length -gt 0 } |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
}

$logFile = Join-Path $LogDir ("watch_{0}.log" -f $Day)
if (-not (Test-Path -LiteralPath $logFile)) {
    $fallback = Get-LatestLogFile -Dir $LogDir
    if ($fallback) {
        Write-Host ("log file {0} not found, using latest: {1}" -f $logFile, $fallback.FullName) -ForegroundColor Yellow
        Copy-Item -LiteralPath $fallback.FullName -Destination $logFile -Force
    } else {
        $altLog = Prompt-Input -Message "Log file not found. Enter full path to an existing log file (or leave blank to send without log):"
        if ($altLog -and (Test-Path -LiteralPath $altLog)) {
            Write-Host ("using user-provided log file: {0}" -f $altLog) -ForegroundColor Yellow
            Copy-Item -LiteralPath $altLog -Destination $logFile -Force
        } else {
            Write-Warning ("log file missing and no fallback provided; sending without log content: {0}" -f $logFile)
            New-Item -ItemType File -Path $logFile -Force | Out-Null
        }
    }
}

$logInfo = Get-Item -LiteralPath $logFile
if ($logInfo.Length -le 0) {
    $fallback = Get-LatestLogFile -Dir $LogDir
    if ($fallback -and $fallback.FullName -ne $logFile) {
        Write-Host ("log file {0} is empty, using latest: {1}" -f $logFile, $fallback.FullName) -ForegroundColor Yellow
        Copy-Item -LiteralPath $fallback.FullName -Destination $logFile -Force
    } else {
        $altLog = Prompt-Input -Message "Log file is empty. Enter full path to a non-empty log file (or leave blank to send without log):"
        if ($altLog -and (Test-Path -LiteralPath $altLog)) {
            $altInfo = Get-Item -LiteralPath $altLog
            if ($altInfo.Length -gt 0) {
                Write-Host ("using user-provided non-empty log file: {0}" -f $altLog) -ForegroundColor Yellow
                Copy-Item -LiteralPath $altLog -Destination $logFile -Force
            } else {
                Write-Warning ("user-provided file is empty; sending without log content: {0}" -f $altLog)
            }
        } else {
            Write-Warning ("log file is empty and no non-empty fallback provided; sending without log content: {0}" -f $logFile)
        }
    }
}

if (-not $Subject) { $Subject = "XWatch log for $Day" }
if (-not $Body)    { $Body    = "Attached: watch log for $Day (from xwatch-watch-logs)." }

$repoRoot = (Split-Path $PSScriptRoot -Parent)
$exePath = Join-Path $repoRoot 'xwatch.exe'
if (-not (Test-Path -LiteralPath $exePath)) {
    Write-Error ('xwatch.exe not found. Please run build.ps1 in repo root. (path: {0})' -f $exePath)
    exit 1
}

$scriptPath = Join-Path $PSScriptRoot 'send-test-mail.ps1'

if ($RegisterTask) {
    $tzId = 'Taipei Standard Time'
    try {
        $tz = [TimeZoneInfo]::FindSystemTimeZoneById($tzId)
    } catch {
        Write-Warning ("timezone '{0}' not found, fallback to local timezone" -f $tzId)
        $tz = [TimeZoneInfo]::Local
    }

    $todayStr = (Get-Date -Format 'yyyy-MM-dd')
    $utc8Target = [DateTime]::ParseExact("$todayStr 10:00", 'yyyy-MM-dd HH:mm', $null)
    $utc8Target = [DateTime]::SpecifyKind($utc8Target, [DateTimeKind]::Unspecified)
    $localTime = [TimeZoneInfo]::ConvertTime($utc8Target, $tz, [TimeZoneInfo]::Local)
    $atLocal = $localTime.ToString('HH:mm')

    $psPath = (Get-Command powershell.exe).Source
    $taskArgs = ('-ExecutionPolicy Bypass -File "{0}" -ConfigPath "{1}" -User "{2}" -AppPassword "{3}" -To "{4}" -Day "{5}" -LogDir "{6}" -Subject "{7}" -Body "{8}" -Host "{9}" -Port {10}' -f $scriptPath, $ConfigPath, $User, $AppPassword, $To, $Day, $LogDir, $Subject, $Body, $SmtpHost, $SmtpPort)

    $action = New-ScheduledTaskAction -Execute $psPath -Argument $taskArgs -WorkingDirectory $repoRoot
    $trigger = New-ScheduledTaskTrigger -Daily -At $atLocal

    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Force | Out-Null
    Write-Host "Scheduled daily run at local $atLocal (UTC+8 10:00), task name: $TaskName" -ForegroundColor Green
    Write-Host "Schedule uses current parameters/environment." -ForegroundColor Green
    return
}

$arguments = @(
    'mail',
    '--host', $SmtpHost,
    '--port', $SmtpPort,
    '--to', $To,
    '--user', $User,
    '--pass', $AppPassword,
    '--day', $Day,
    '--log-dir', $LogDir,
    '--subject', $Subject,
    '--body', $Body
)

Write-Host "Running: xwatch.exe mail" -ForegroundColor Cyan
& $exePath @arguments

$exit = $LASTEXITCODE
if ($exit -eq 0) {
    Write-Host "Mail command finished successfully (exit 0)." -ForegroundColor Green
} else {
    Write-Error ("Mail command failed, exit code {0}. See output above." -f $exit)
    exit $exit
}
