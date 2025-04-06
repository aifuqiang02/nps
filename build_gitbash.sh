#!/bin/bash
# NPS/NPC build script for Git Bash on Windows

# 设置变量
BUILD_DIR="bin"
WEB_DIR="web"
CONF_DIR="conf"
PLATFORMS=("windows/amd64" "linux/amd64")

# 清理旧构建
echo "Cleaning previous builds..."
rm -rf $BUILD_DIR
mkdir -p $BUILD_DIR

# 编译nps和npc
for PLATFORM in "${PLATFORMS[@]}"; do
  OS=$(echo $PLATFORM | cut -d'/' -f1)
  ARCH=$(echo $PLATFORM | cut -d'/' -f2)
  
  echo "Building nps for $OS/$ARCH..."
  GOOS=$OS GOARCH=$ARCH go build -o $BUILD_DIR/nps-$OS-$ARCH cmd/nps/nps.go
  
  echo "Building npc for $OS/$ARCH..."
  GOOS=$OS GOARCH=$ARCH go build -o $BUILD_DIR/npc-$OS-$ARCH cmd/npc/npc.go
done

# 复制资源文件
echo "Copying web resources..."
cp -r $WEB_DIR $BUILD_DIR/
cp -r $CONF_DIR $BUILD_DIR/

# 生成Windows可执行文件
echo "Generating Windows executables..."
mv $BUILD_DIR/nps-windows-amd64 $BUILD_DIR/nps.exe
mv $BUILD_DIR/npc-windows-amd64 $BUILD_DIR/npc.exe

echo "Build completed successfully!"
