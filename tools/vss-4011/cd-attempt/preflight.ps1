$ErrorActionPreference='Stop'
$r=[ordered]@{schema=1;utc=[DateTime]::UtcNow.ToString('o');valid=$false;shadow_count=-1;c_shadow_count=-1;d_shadow_count=-1;restore_point_count=-1;association_count=-1;c_to_c=$false;c_to_d=$false;maximum_bytes=0;used_bytes=0;allocated_bytes=0;c_free_bytes=0;d_free_bytes=0;ntfs=$false;distinct_nvme=$false;d_online_writable_fixed=$false;only_microsoft_provider=$false;protection_not_disabled=$false;minimum_unset=$false;hresult=$null}
try {
    $v=@(Get-CimInstance Win32_Volume -Filter "DriveLetter='C:' OR DriveLetter='D:'" -Property DeviceID,DriveLetter,FileSystem,FreeSpace,DriveType)
    $c=@($v|Where-Object {$_.DriveLetter -eq 'C:'})
    $d=@($v|Where-Object {$_.DriveLetter -eq 'D:'})
    if($c.Count -ne 1 -or $d.Count -ne 1){throw 'VOLUMES_NOT_UNIQUE'}
    $r.ntfs=$c[0].FileSystem -eq 'NTFS' -and $d[0].FileSystem -eq 'NTFS'
    $r.c_free_bytes=[uint64]$c[0].FreeSpace
    $r.d_free_bytes=[uint64]$d[0].FreeSpace
    $s=@(Get-CimInstance Win32_ShadowCopy)
    $r.shadow_count=$s.Count
    $r.c_shadow_count=@($s|Where-Object {$_.VolumeName -eq $c[0].DeviceID}).Count
    $r.d_shadow_count=@($s|Where-Object {$_.VolumeName -eq $d[0].DeviceID}).Count
    $r.restore_point_count=@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Property SequenceNumber).Count
    $a=@(Get-CimInstance Win32_ShadowStorage)
    $r.association_count=$a.Count
    if($a.Count -eq 1){
        $r.c_to_c=$a[0].Volume.DeviceID -eq $c[0].DeviceID -and $a[0].DiffVolume.DeviceID -eq $c[0].DeviceID
        $r.c_to_d=$a[0].Volume.DeviceID -eq $c[0].DeviceID -and $a[0].DiffVolume.DeviceID -eq $d[0].DeviceID
        $r.maximum_bytes=[uint64]$a[0].MaxSpace
        $r.used_bytes=[uint64]$a[0].UsedSpace
        $r.allocated_bytes=[uint64]$a[0].AllocatedSpace
    }
    $cp=@(Get-Partition -DriveLetter C);$dp=@(Get-Partition -DriveLetter D)
    if($cp.Count -ne 1 -or $dp.Count -ne 1){throw 'PARTITIONS_NOT_UNIQUE'}
    $cd=Get-Disk -Number $cp[0].DiskNumber;$dd=Get-Disk -Number $dp[0].DiskNumber
    $sameSerial=[string]::IsNullOrWhiteSpace($cd.SerialNumber) -or [string]::IsNullOrWhiteSpace($dd.SerialNumber) -or $cd.SerialNumber.Trim() -ceq $dd.SerialNumber.Trim()
    $r.distinct_nvme=$cd.BusType -eq 'NVMe' -and $dd.BusType -eq 'NVMe' -and $cd.Number -ne $dd.Number -and -not $sameSerial
    $r.d_online_writable_fixed=$d[0].DriveType -eq 3 -and $dd.HealthStatus -eq 'Healthy' -and $dd.OperationalStatus -contains 'Online' -and -not $dd.IsOffline -and -not $dd.IsReadOnly -and -not $dd.IsSystem -and -not $dd.IsBoot
    $providers=@(Get-CimInstance Win32_ShadowProvider -Property Name)
    $r.only_microsoft_provider=$providers.Count -eq 1 -and $providers[0].Name -eq 'Microsoft Software Shadow Copy provider 1.0'
    $disabled=$false
    foreach($path in @('HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\SystemRestore','HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\SystemRestore')){
        $value=Get-ItemProperty -LiteralPath $path -Name DisableSR -ErrorAction SilentlyContinue
        if($value -and $value.DisableSR -ne 0){$disabled=$true}
    }
    $r.protection_not_disabled=-not $disabled
    $key=Get-Item -LiteralPath 'HKLM:\SYSTEM\CurrentControlSet\Services\VolSnap'
    $r.minimum_unset='MinDiffAreaFileSize' -notin $key.GetValueNames()
    $r.valid=$true
}catch{$r.hresult=$_.Exception.HResult}
$r|ConvertTo-Json -Depth 4
