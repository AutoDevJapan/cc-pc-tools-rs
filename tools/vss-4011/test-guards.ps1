param([Parameter(Mandatory=$true)][string]$ScratchPath)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
function Sample($allocated,$used,$maximum=50GB) {return [pscustomobject]@{source='C:';storage='C:';allocated_bytes=[int64]$allocated;used_bytes=[int64]$used;maximum_bytes=[int64]$maximum}}
$zero=Sample 0 0
$cases=@(
    @{name='fresh-reservation';before=$zero;after=(Sample 3200MB 64MB);expected=$true},
    @{name='boundary-unused-reserve';before=$zero;after=(Sample 3200MB 320MB);expected=$true},
    @{name='natural-used-growth-is-not-reservation';before=$zero;after=(Sample 3200MB 3100MB);expected=$false},
    @{name='absolute-allocation-already-large';before=(Sample 6GB 4GB);after=(Sample (6GB+128MB) (4GB+64MB));expected=$false},
    @{name='insufficient-initial-allocation';before=$zero;after=(Sample 512MB 64MB);expected=$false},
    @{name='insufficient-unused-reserve';before=$zero;after=(Sample 3200MB 400MB);expected=$false},
    @{name='maximum-changed';before=$zero;after=(Sample 3200MB 64MB 51GB);expected=$false},
    @{name='used-space-decreased';before=(Sample 1GB 512MB);after=(Sample (1GB+3200MB) 64MB);expected=$false}
)
foreach($case in $cases) {
    $actual=Test-Vss4011InitialReservation $case.before $case.after
    if($actual.passed -ne $case.expected) {throw ('RESERVATION_GATE_REGRESSION_'+$case.name)}
    Write-Output ('passed '+$case.name)
}
# Real filesystem check: a crash's permanent request must survive another call.
$claim=Join-Path $ScratchPath 'immutable-request.private.json'
Write-Vss4011NewReceipt $claim ([ordered]@{phase='begin_call_pending_outcome_unknown';synthetic=$true})
$hashBefore=Get-Vss4011FileHash $claim
$rejected=$false
try {Write-Vss4011NewReceipt $claim ([ordered]@{phase='incorrect-second-attempt';synthetic=$true})} catch {$rejected=$_.Exception.GetBaseException() -is [IO.IOException]}
if(-not $rejected -or (Get-Vss4011FileHash $claim) -ne $hashBefore) {throw 'PERMANENT_REQUEST_WAS_OVERWRITTEN'}
Write-Output 'passed permanent-request-blocks-retry'
$rejected=$false
try {Get-Vss4011ReceiptPath 'point-A-result' (Join-Path $ScratchPath 'different-result.private.json') | Out-Null} catch {$rejected=$_.Exception.Message -eq 'FIXED_RECEIPT_PATH_REQUIRED'}
if(-not $rejected) {throw 'ALTERNATE_OUTPUT_PATH_BYPASSED_STAGE_CLAIM'}
Write-Output 'passed fixed-stage-path'
Write-Output '10 guards passed; no registry, VSS, or SystemRestore calls executed'
