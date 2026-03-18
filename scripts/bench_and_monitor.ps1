param(
  [string]$ProcessName = "redigo-server",
  [string]$BenchExe = ".\\bin\\redigo-bench.exe",
  [string[]]$BenchArgs = @(),
  [int]$SampleSeconds = 1,
  [string]$OutPath = ".\\bench-results\\bench_out.txt",
  [string]$ErrPath = ".\\bench-results\\bench_err.txt",
  [string]$CsvPath = ".\\bench-results\\metrics.csv"
)

$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force (Split-Path -Parent $OutPath) | Out-Null
New-Item -ItemType Directory -Force (Split-Path -Parent $CsvPath) | Out-Null

if (Test-Path $CsvPath) { Remove-Item $CsvPath -Force }
"ts,cpu_pct,ws_mb,pm_mb,io_read_mb,io_write_mb,threads,handles" | Out-File -FilePath $CsvPath -Encoding ascii

$normArgs = @()
for ($i = 0; $i -lt $BenchArgs.Count; $i++) {
  $a = [string]$BenchArgs[$i]
  if ($a.StartsWith("-") -and (-not $a.Contains("=")) -and ($i + 1 -lt $BenchArgs.Count)) {
    $b = [string]$BenchArgs[$i + 1]
    if (-not $b.StartsWith("-")) {
      $normArgs += ($a + "=" + $b)
      $i++
      continue
    }
  }
  $normArgs += $a
}

$bench = Start-Process -FilePath $BenchExe -ArgumentList ($normArgs -join ' ') -NoNewWindow -PassThru -RedirectStandardOutput $OutPath -RedirectStandardError $ErrPath

$prevCpu = $null
$prevT = $null

while (-not $bench.HasExited) {
  $p = Get-Process -Name $ProcessName -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($p) {
    $now = Get-Date
    if ($prevCpu -ne $null -and $prevT -ne $null) {
      $dt = ($now - $prevT).TotalSeconds
      if ($dt -le 0) { $dt = 1 }
      $cores = [int]$env:NUMBER_OF_PROCESSORS
      if ($cores -le 0) { $cores = 1 }
      $cpuPct = ((($p.CPU - $prevCpu) / $dt) / $cores) * 100
      if ($cpuPct -lt 0) { $cpuPct = 0 }
      if ($cpuPct -gt 100) { $cpuPct = 100 }
      $ws = [math]::Round($p.WorkingSet64 / 1MB, 2)
      $pm = [math]::Round($p.PrivateMemorySize64 / 1MB, 2)
      $ior = [math]::Round($p.IOReadBytes / 1MB, 2)
      $iow = [math]::Round($p.IOWriteBytes / 1MB, 2)
      $line = ([DateTime]::UtcNow.ToString("o")) + "," + ([math]::Round($cpuPct, 2)) + "," + $ws + "," + $pm + "," + $ior + "," + $iow + "," + $p.Threads.Count + "," + $p.HandleCount
      Add-Content -Path $CsvPath -Value $line
    }
    $prevCpu = $p.CPU
    $prevT = $now
  }
  Start-Sleep -Seconds $SampleSeconds
}

$bench.WaitForExit()
Get-Content $OutPath

$rows = Import-Csv $CsvPath
if ($rows -and $rows.Count -gt 0) {
  $cpuMax = ($rows | Measure-Object -Property cpu_pct -Maximum).Maximum
  $wsMax = ($rows | Measure-Object -Property ws_mb -Maximum).Maximum
  $pmMax = ($rows | Measure-Object -Property pm_mb -Maximum).Maximum
  $ioReadMax = ($rows | Measure-Object -Property io_read_mb -Maximum).Maximum
  $ioWriteMax = ($rows | Measure-Object -Property io_write_mb -Maximum).Maximum
  $cpuAvg = ($rows | Measure-Object -Property cpu_pct -Average).Average
  $wsAvg = ($rows | Measure-Object -Property ws_mb -Average).Average
  $pmAvg = ($rows | Measure-Object -Property pm_mb -Average).Average
  Write-Output ""
  Write-Output ("metrics_cpu_pct_max=" + ([math]::Round([double]$cpuMax, 2)))
  Write-Output ("metrics_cpu_pct_avg=" + ([math]::Round([double]$cpuAvg, 2)))
  Write-Output ("metrics_ws_mb_max=" + ([math]::Round([double]$wsMax, 2)))
  Write-Output ("metrics_ws_mb_avg=" + ([math]::Round([double]$wsAvg, 2)))
  Write-Output ("metrics_pm_mb_max=" + ([math]::Round([double]$pmMax, 2)))
  Write-Output ("metrics_pm_mb_avg=" + ([math]::Round([double]$pmAvg, 2)))
  Write-Output ("metrics_io_read_mb_max=" + ([math]::Round([double]$ioReadMax, 2)))
  Write-Output ("metrics_io_write_mb_max=" + ([math]::Round([double]$ioWriteMax, 2)))
}
