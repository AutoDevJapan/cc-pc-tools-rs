@echo off
"%~dp0..\cargo_gate.exe" %*
exit /b %ERRORLEVEL%
