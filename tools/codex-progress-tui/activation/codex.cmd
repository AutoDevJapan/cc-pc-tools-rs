@echo off
rem opencodex codex autostart shim
setlocal
set "OCX_REAL_CODEX=D:\CC\Tools\codex-progress-tui\codex-launch.exe"
set "OCX_BUN=%APPDATA%\npm\node_modules\@bitkyc08\opencodex\node_modules\bun\bin\bun.exe"
set "OCX_CLI=%APPDATA%\npm\node_modules\@bitkyc08\opencodex\src\cli\index.ts"
set "OCX_API_TOKEN_FILE=%USERPROFILE%\.opencodex\service-api-token"
if "%OPENCODEX_API_AUTH_TOKEN%"=="" if exist "%OCX_API_TOKEN_FILE%" set /p OPENCODEX_API_AUTH_TOKEN=<"%OCX_API_TOKEN_FILE%"
if not "%OCX_SHIM_BYPASS%"=="" goto run_codex
goto scan_codex_args
:scan_codex_args
if "%~1"=="" goto ensure_ocx
if "%~1"=="--" goto ensure_ocx
if /I "%~1"=="-c" goto skip_option_value
if /I "%~1"=="--config" goto skip_option_value
if /I "%~1"=="--enable" goto skip_option_value
if /I "%~1"=="--disable" goto skip_option_value
if /I "%~1"=="--remote" goto skip_option_value
if /I "%~1"=="--remote-auth-token-env" goto skip_option_value
if /I "%~1"=="-i" goto skip_option_value
if /I "%~1"=="--image" goto skip_option_value
if /I "%~1"=="-m" goto skip_option_value
if /I "%~1"=="--model" goto skip_option_value
if /I "%~1"=="--local-provider" goto skip_option_value
if /I "%~1"=="-p" goto skip_option_value
if /I "%~1"=="--profile" goto skip_option_value
if /I "%~1"=="-s" goto skip_option_value
if /I "%~1"=="--sandbox" goto skip_option_value
if /I "%~1"=="-C" goto skip_option_value
if /I "%~1"=="--cd" goto skip_option_value
if /I "%~1"=="--add-dir" goto skip_option_value
if /I "%~1"=="-a" goto skip_option_value
if /I "%~1"=="--ask-for-approval" goto skip_option_value
if /I "%~1"=="app-server" goto run_codex
if /I "%~1"=="progress" goto run_codex
if /I "%~1"=="archive" goto run_codex
if /I "%~1"=="apply" goto run_codex
if /I "%~1"=="cloud" goto run_codex
if /I "%~1"=="completion" goto run_codex
if /I "%~1"=="debug" goto run_codex
if /I "%~1"=="delete" goto run_codex
if /I "%~1"=="doctor" goto run_codex
if /I "%~1"=="exec-server" goto run_codex
if /I "%~1"=="features" goto run_codex
if /I "%~1"=="fork" goto run_codex
if /I "%~1"=="help" goto run_codex
if /I "%~1"=="login" goto run_codex
if /I "%~1"=="logout" goto run_codex
if /I "%~1"=="mcp" goto run_codex
if /I "%~1"=="plugin" goto run_codex
if /I "%~1"=="sandbox" goto run_codex
if /I "%~1"=="unarchive" goto run_codex
if /I "%~1"=="update" goto run_codex
if /I "%~1"=="--help" goto run_codex
if /I "%~1"=="-h" goto run_codex
if /I "%~1"=="--version" goto run_codex
if /I "%~1"=="-V" goto run_codex
set "OCX_SCAN_ARG=%~1"
if "%OCX_SCAN_ARG:~0,1%"=="-" goto shift_codex_arg
goto ensure_ocx
:skip_option_value
shift
if "%~1"=="" goto ensure_ocx
:shift_codex_arg
shift
goto scan_codex_args
:ensure_ocx
setlocal
set "OCX_BUN_RUNTIME_SOURCE=bundled"
set "OCX_BUN_RUNTIME_PATH=%APPDATA%\npm\node_modules\@bitkyc08\opencodex\node_modules\bun\bin\bun.exe"
"%OCX_BUN%" "%OCX_CLI%" ensure >nul 2>nul
if errorlevel 1 echo opencodex: proxy autostart failed; launching Codex anyway. Run 'ocx doctor' for details, or 'ocx restore' to hand Codex back to its own account. 1>&2
endlocal
:run_codex
if not exist "%OCX_REAL_CODEX%" set "OCX_REAL_CODEX=%APPDATA%\npm\codex.opencodex-real.cmd"
"%OCX_REAL_CODEX%" %*
