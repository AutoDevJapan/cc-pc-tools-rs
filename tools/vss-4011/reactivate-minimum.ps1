param(
    [Parameter(Mandatory=$true)][ValidateSet('Apply','Undo')][string]$Mode,
    [Parameter(Mandatory=$true)][string]$QueryExe,
    [Parameter(Mandatory=$true)][string]$QueryExeSha256
)
# Retained entry point for a retired experiment. The original operation and
# rollback have finished; another checkout must not repeat them.
throw 'RETIRED_VSS_EXPERIMENT_NO_STATE_CHANGE'
