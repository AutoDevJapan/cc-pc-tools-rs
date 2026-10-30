param([Parameter(Mandatory=$true)][ValidateSet('A','B')][string]$Stage,[string]$OutputPath,[string]$FirstReceiptPath)
# Retained entry point for a retired experiment. The original operation and
# rollback have finished; another checkout must not repeat them.
throw 'RETIRED_VSS_EXPERIMENT_NO_STATE_CHANGE'
