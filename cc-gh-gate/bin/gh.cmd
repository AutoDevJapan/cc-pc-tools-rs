@echo off
"%~dp0..\gh_gate.exe" %*
exit /b %ERRORLEVEL%
