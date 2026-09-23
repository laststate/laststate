@echo off
REM Start the Trace web UI in mock/preview mode with fake data.
REM No database, S3, or queue required - perfect for UI development and demos.
REM
REM Usage:
REM   scripts\mock-preview.bat
REM
REM Opens: http://localhost:8080

setlocal

echo Starting Trace in MOCK mode...
echo   - No database required
echo   - No S3/object store required
echo   - No queue/worker required
echo   - UI preview with realistic fake data
echo.

REM Build web UI if dist doesn't exist
if not exist "%~dp0..\web\dist" (
    echo Building web UI...
    cd "%~dp0..\web"
    call npm run build
    cd "%~dp0.."
    echo Web UI built successfully.
    echo.
)

echo   Open: http://localhost:8080
echo   Press Ctrl+C to stop
echo.

set TRACE_MOCK=true
set TRACE_OPEN_UI=true
set TRACE_WEB_DIR=%~dp0..\web\dist

go run .\cmd\trace

endlocal
