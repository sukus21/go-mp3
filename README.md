# (a fork of) go-mp3
Looking through the internals of this package, I noticed a few things I would like.
Primarily just exposes a few properties on the decoder through getter methods, that weren't previously exposed.
This fork is for personal use.

All files have had their package namespace updated.

 ---

# go-mp3

[![Go Reference](https://pkg.go.dev/badge/github.com/hajimehoshi/go-mp3.svg)](https://pkg.go.dev/github.com/hajimehoshi/go-mp3) 

An MP3 decoder in pure Go based on [PDMP3](https://github.com/technosaurus/PDMP3).

[Slide at golang.tokyo #11](https://docs.google.com/presentation/d/e/2PACX-1vTTXf-LWNRvMVGQ7GI4Wh8EKohot_9CMtlF4dswpYGpuYKOek5NeNP-_QZnNcRFZp9Cwm0pCcykjqDN/pub?start=false&loop=false&delayms=3000)
