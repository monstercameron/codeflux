# Packaged browser assets

`codeflux-dev generate` writes the built browser assets here, and a release
build embeds them into the executable (M23-053). In a development checkout the
directory holds only this file, and the frontend server falls back to reading
the build directory instead.

This file exists because Go cannot embed an empty directory. It is deliberately
not one of the required assets, so its presence never makes an incomplete asset
set look complete.
