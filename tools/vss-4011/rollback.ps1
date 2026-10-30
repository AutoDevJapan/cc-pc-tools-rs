param([string]$AppliedReceiptPath,[string]$OutputPath)
# Retained entry point for a retired experiment. The original operation and
# rollback have finished; another checkout must not repeat them.
throw 'RETIRED_VSS_EXPERIMENT_NO_STATE_CHANGE'
