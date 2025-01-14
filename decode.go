// Copyright 2017 Hajime Hoshi
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// CHANGES IN DERIVATIVE VERSION (by sukus):
// * Exposed `decoder.bytesPerFrame` through `(*Decoder).BytesPerFrame()`.
// * Added custom seek method, to seek using percentage.
// * Added volume property to Decoder.
// * Added getter/setters for `Decoder.volume`.

package mp3

import (
	"errors"
	"io"
	"sync"

	"github.com/sukus21/go-mp3/internal/consts"
	"github.com/sukus21/go-mp3/internal/frame"
	"github.com/sukus21/go-mp3/internal/frameheader"
)

// A Decoder is a MP3-decoded stream.
//
// Decoder decodes its underlying source on the fly.
type Decoder struct {
	source        *source
	sampleRate    int
	length        int64
	frameStarts   []int64
	buf           []byte
	frame         *frame.Frame
	pos           int64
	bytesPerFrame int64
	mux           sync.Mutex
	volume        float32
}

// !!! NEW TO DERIVATIVE WORK !!!
// Exposes how many bytes are in a single frame.
func (d *Decoder) BytesPerFrame() int64 {
	return d.bytesPerFrame
}

func (d *Decoder) readFrame() error {
	var err error
	d.frame, _, err = frame.Read(d.source, d.source.pos, d.frame)
	if err != nil {
		if err == io.EOF {
			return io.EOF
		}
		if _, ok := err.(*consts.UnexpectedEOF); ok {
			// TODO: Log here?
			return io.EOF
		}
		return err
	}
	d.buf = append(d.buf, d.frame.Decode(d.volume)...)
	return nil
}

// Read is io.Reader's Read.
func (d *Decoder) Read(buf []byte) (int, error) {
	d.mux.Lock()
	defer d.mux.Unlock()
	for len(d.buf) == 0 {
		if err := d.readFrame(); err != nil {
			return 0, err
		}
	}
	n := copy(buf, d.buf)
	d.buf = d.buf[n:]
	d.pos += int64(n)
	return n, nil
}

// Seek is io.Seeker's Seek.
//
// Seek returns an error when the underlying source is not io.Seeker.
//
// Note that seek uses a byte offset but samples are aligned to 4 bytes (2
// channels, 2 bytes each). Be careful to seek to an offset that is divisible by
// 4 if you want to read at full sample boundaries.
func (d *Decoder) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekCurrent {
		// Handle the special case of asking for the current position specially.
		return d.pos, nil
	}

	npos := int64(0)
	switch whence {
	case io.SeekStart:
		npos = offset
	case io.SeekCurrent:
		npos = d.pos + offset
	case io.SeekEnd:
		npos = d.Length() + offset
	default:
		return 0, errors.New("mp3: invalid whence")
	}
	d.pos = npos
	d.buf = nil
	d.frame = nil
	f := d.pos / d.bytesPerFrame
	// If the frame is not first, read the previous ahead of reading that
	// because the previous frame can affect the targeted frame.
	if f > 0 {
		f--
		if _, err := d.source.Seek(d.frameStarts[f], 0); err != nil {
			return 0, err
		}
		if err := d.readFrame(); err != nil {
			return 0, err
		}
		if err := d.readFrame(); err != nil {
			return 0, err
		}
		d.buf = d.buf[d.bytesPerFrame+(d.pos%d.bytesPerFrame):]
	} else {
		if _, err := d.source.Seek(d.frameStarts[f], 0); err != nil {
			return 0, err
		}
		if err := d.readFrame(); err != nil {
			return 0, err
		}
		d.buf = d.buf[d.pos:]
	}
	return npos, nil
}

// !!! NEW TO DERIVATIVE WORK !!!
// Seek to a normalized position.
// This is possible, because we already have every frames position in the stream.
//
// Note that this function seeks to frame boundaries.
//
// This returns an error when the underlying source is not io.Seeker.
func (d *Decoder) SeekPercent(where float64) (int64, error) {
	//Don't seek out of bounds
	if where < 0 {
		return 0, errors.New("mp3: cannot seek to negative position")
	}
	if where > 1 {
		return 0, errors.New("mp3: cannot seek beyond stream end")
	}

	//Don't read while seeking
	d.mux.Lock()
	defer d.mux.Unlock()

	//How far is it acceptable to go back?
	offset := 4
	frameIndex := int64(float64(len(d.frameStarts))*where) - int64(offset)
	for frameIndex < 0 {
		frameIndex++
		offset--
	}
	d.frame = nil

	//Seek in source buffer
	d.pos = d.bytesPerFrame * frameIndex
	if _, err := d.source.Seek(d.frameStarts[frameIndex], io.SeekStart); err != nil {
		return 0, err
	}

	//Pre-read some frames, to avoid artifacting
	for i := 0; i < offset; i++ {
		if frameIndex >= 0 {
			d.buf = d.buf[:0]
			if err := d.readFrame(); err != nil {
				return 0, err
			}
		}
		frameIndex++
	}

	return d.pos, nil
}

// SampleRate returns the sample rate like 44100.
//
// Note that the sample rate is retrieved from the first frame.
func (d *Decoder) SampleRate() int {
	return d.sampleRate
}

func (d *Decoder) ensureFrameStartsAndLength() error {
	if d.length != invalidLength {
		return nil
	}

	if _, ok := d.source.reader.(io.Seeker); !ok {
		return nil
	}

	// Keep the current position.
	pos, err := d.source.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if err := d.source.rewind(); err != nil {
		return err
	}

	if err := d.source.skipTags(); err != nil {
		return err
	}
	l := int64(0)
	for {
		h, pos, err := frameheader.Read(d.source, d.source.pos)
		if err != nil {
			if err == io.EOF {
				break
			}
			if _, ok := err.(*consts.UnexpectedEOF); ok {
				// TODO: Log here?
				break
			}
			return err
		}
		d.frameStarts = append(d.frameStarts, pos)
		d.bytesPerFrame = int64(h.BytesPerFrame())
		l += d.bytesPerFrame

		framesize, err := h.FrameSize()
		if err != nil {
			return err
		}
		buf := make([]byte, framesize-4)
		if _, err := d.source.ReadFull(buf); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	d.length = l

	if _, err := d.source.Seek(pos, io.SeekStart); err != nil {
		return err
	}
	return nil
}

const invalidLength = -1

// Length returns the total size in bytes.
//
// Length returns -1 when the total size is not available
// e.g. when the given source is not io.Seeker.
func (d *Decoder) Length() int64 {
	return d.length
}

// !!! NEW TO DERIVATIVE WORK !!!
//
// Get the scale of volume.
// This value should always be between 0 and 1.
func (d *Decoder) GetVolume() float32 {
	return d.volume
}

// !!! NEW TO DERIVATIVE WORK !!!
//
// Set the scale of volume.
// This value will be clamped to a range between 0 and 1.
func (d *Decoder) SetVolume(volume float32) {
	if volume > 1 {
		volume = 1
	} else if volume < 0 {
		volume = 0
	}
	d.volume = volume
}

// NewDecoder decodes the given io.Reader and returns a decoded stream.
//
// The stream is always formatted as 16bit (little endian) 2 channels
// even if the source is single channel MP3.
// Thus, a sample always consists of 4 bytes.
func NewDecoder(r io.Reader) (*Decoder, error) {
	s := &source{
		reader: r,
	}
	d := &Decoder{
		source: s,
		length: invalidLength,
	}

	if err := s.skipTags(); err != nil {
		return nil, err
	}
	// TODO: Is readFrame here really needed?
	if err := d.readFrame(); err != nil {
		return nil, err
	}
	freq, err := d.frame.SamplingFrequency()
	if err != nil {
		return nil, err
	}
	d.sampleRate = freq

	if err := d.ensureFrameStartsAndLength(); err != nil {
		return nil, err
	}

	d.volume = 1
	return d, nil
}
