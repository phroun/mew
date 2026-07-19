#!/bin/bash
# CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
#  CC=x86_64-w64-mingw32-gcc \
#  CGO_CFLAGS="-I$HOME/projects/vendor/SDL2-2.32.10/x86_64-w64-mingw32/include" \
#  CGO_LDFLAGS="-L$HOME/projects/vendor/SDL2-2.32.10/x86_64-w64-mingw32/lib" \
#  go build -tags sdl -o kittytk-sdl.exe ./cmd/kittytk-sdl

SDL=$HOME/projects/vendor/SDL2-2.32.10/x86_64-w64-mingw32
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
  CC=x86_64-w64-mingw32-gcc \
  CGO_CFLAGS="-I$SDL/include -DSDL_STATIC" \
  CGO_LDFLAGS="-L$SDL/lib -lSDL2 -lSDL2main \
    -lmingw32 -mwindows \
    -ldinput8 -ldxguid -ldxerr8 -luser32 -lgdi32 -lwinmm -limm32 \
    -lole32 -loleaut32 -lshell32 -lsetupapi -lversion -luuid \
    -lhid -lsetupapi -static-libgcc -static" \
  go build -tags "sdl static" -ldflags "-H windowsgui" \
  -o dist/kittytk-sdl.exe ./cmd/kittytk-sdl
