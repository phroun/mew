#!/bin/bash
clear
loops=100

# perl gives sub-second wall time on macOS (BSD date has no %N)
start=$(perl -MTime::HiRes=time -e 'printf "%.6f", time')

for ((i=1; i<=loops; i++)); do          # C-style loop: expands the variable
    printf '\033[H\033[2J'
    printf 'This is a VT100 framerate test line...'
done

end=$(perl -MTime::HiRes=time -e 'printf "%.6f", time')
fps=$(perl -e "printf '%.1f', $loops / ($end - $start)")
printf '\nRendered %d frames in %.3f s.\nAverage Framerate: %s FPS\n' \
    "$loops" "$(perl -e "print $end - $start")" "$fps"
