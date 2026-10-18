$ErrorActionPreference = 'Stop'
Import-Module Hyper-V
Import-Module Storage
$root = [IO.Path]::GetFullPath('__CCSDK2737_OWNED_ROOT__')
$receiptPath = Join-Path $root 'vm-created.private.json'
$receipt = Get-Content -Raw -LiteralPath $receiptPath | ConvertFrom-Json
if ($receipt.instance -notmatch '^[A-Za-z0-9_-]{1,128}$' -or $receipt.name -cne ('cc-sdk-2737-' + $receipt.instance) -or $receipt.vhd -cne (Join-Path $root 'guest.vhdx') -or $receipt.root -cne $root) { throw 'OWNED_VM_RECEIPT_REJECTED' }
$id = [guid]$receipt.vm_id
$verifiedSDK=$false
$journalPath=Join-Path $root 'sdk-operations.private.json'
if(Test-Path -LiteralPath $journalPath){$journal=Get-Content -Raw -LiteralPath $journalPath|ConvertFrom-Json;$verifiedSDK=($journal.failure -ceq '<nil>' -and @($journal.receipt.results).Count -eq 11 -and @($journal.receipt.results|Where-Object {-not $_.ok}).Count -eq 0)}
$vm = Get-VM -Id $id -ErrorAction Stop
if ($vm.Name -cne $receipt.name -or ($vm.Notes -and $vm.Notes -cne ('todo2783;instance=' + $receipt.instance))) { throw 'OWNED_VM_IDENTITY_REJECTED' }
$mode='already-off'
if ($vm.State -ne 'Off') {
  try { Stop-VM -VM $vm -Confirm:$false -ErrorAction Stop; $mode='guest-integration-shutdown' }
  catch { if($verifiedSDK){throw}; Stop-VM -VM $vm -TurnOff -Confirm:$false; $mode='power-off-incomplete-sdk' }
  $vm = Get-VM -Id $id
  if ($vm.State -ne 'Off') { throw 'OWNED_VM_STOP_FAILED' }
}
if ($vm.MemoryAssigned -ne 0) { throw 'OWNED_VM_MEMORY_NOT_ZERO' }
@{instance=$receipt.instance;vm_id=$id.ToString();vm_state='Off';memory_assigned=0} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $root 'vm-stopped.private.json') -Encoding utf8
$disks = @($vm | Get-VMHardDiskDrive)
if ($disks.Count -ne 1 -or $disks[0].Path -cne $receipt.vhd) { throw 'OWNED_VM_DISK_IDENTITY_REJECTED' }
Remove-VM -VM $vm -Force
if (Get-VM -Id $id -ErrorAction SilentlyContinue) { throw 'OWNED_VM_REMOVAL_FAILED' }
$vhd = Get-VHD -Path $receipt.vhd -ErrorAction Stop
if ($vhd.Attached) { throw 'OWNED_TEMPLATE_VHD_STILL_ATTACHED' }
$template = Get-Item -LiteralPath $receipt.vhd
$hash=''
if($verifiedSDK){$hash=(Get-FileHash -LiteralPath $receipt.vhd -Algorithm SHA256).Hash}
$result = @{instance=$receipt.instance;vm_id=$id.ToString();vm_state='Off';memory_assigned=0;vm_removed=$true;shutdown_mode=$mode;sdk_verified=$verifiedSDK;vhd=$receipt.vhd;vhd_detached=$true;vhd_size=$template.Length;vhd_sha256=$hash}
$result | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $root 'vm-recovery.private.json') -Encoding utf8
