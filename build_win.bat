@echo off
setlocal enabledelayedexpansion

:: 设置版本号和环境变量
set VERSION=0.26.22
set GOPROXY=direct

:: 安装必要的编译工具
echo Installing required tools...
choco install -y mingw make git
if errorlevel 1 (
    echo Failed to install required tools
    exit /b 1
)

:: 创建输出目录
mkdir build_output 2>nul

:: 编译Windows平台
echo Building Windows executables...
go build -ldflags "-s -w -extldflags -static" -o build_output\nps.exe cmd/nps/nps.go
go build -ldflags "-s -w -extldflags -static" -o build_output\npc.exe cmd/npc/npc.go

:: 交叉编译Linux平台
echo Building Linux executables...
set GOOS=linux
set CGO_ENABLED=0

for %%a in (amd64 386 arm arm64) do (
    echo Building Linux_%%a...
    set GOARCH=%%a
    if "%%a"=="arm" set GOARM=7
    go build -ldflags "-s -w -extldflags -static" -o build_output\nps_linux_%%a cmd/nps/nps.go
    go build -ldflags "-s -w -extldflags -static" -o build_output\npc_linux_%%a cmd/npc/npc.go
)

:: 交叉编译MacOS平台
echo Building Darwin executables...
set GOOS=darwin
for %%a in (amd64 arm64) do (
    echo Building Darwin_%%a...
    set GOARCH=%%a
    go build -ldflags "-s -w -extldflags -static" -o build_output\nps_darwin_%%a cmd/nps/nps.go
    go build -ldflags "-s -w -extldflags -static" -o build_output\npc_darwin_%%a cmd/npc/npc.go
)

:: 打包资源文件
echo Packaging resources...
mkdir build_output\web 2>nul
xcopy /E /Y web\static build_output\web\static
xcopy /E /Y web\views build_output\web\views
xcopy /E /Y conf build_output\conf

:: 创建压缩包
echo Creating archives...
cd build_output
for %%a in (*.exe) do (
    7z a -tzip %%a.zip %%a conf\* web\*
)
for %%a in (nps_* npc_*) do (
    7z a -tzip %%a.zip %%a conf\* web\*
)

echo Build completed successfully!
endlocal
