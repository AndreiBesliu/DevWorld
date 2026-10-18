import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

// The NOTICE file is this package's only third-party licence artefact (spec 6.2
// and the repo layout: the player is adapted from video-rtc.js, "atribuire in
// NOTICE"). These tests keep the attribution from being dropped or shortened.

const notice = readFileSync(resolve(import.meta.dirname, '../NOTICE'), 'utf8');

// NOTICE indents and re-wraps the licence text, so compare words, not layout.
const words = (text: string): string => text.split(/\s+/).filter(Boolean).join(' ');

// The licence of go2rtc, which hosts video-rtc.js (www/video-rtc.js), copied
// verbatim from https://github.com/AlexxIT/go2rtc/blob/master/LICENSE.
const GO2RTC_LICENSE = `MIT License

Copyright (c) 2022 Alexey Khit

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.`;

describe('NOTICE', () => {
  it('notice_videoRtc_carriesGo2rtcCopyrightLineAndFullMitText', () => {
    expect(words(notice)).toContain(words(GO2RTC_LICENSE));
  });

  it('notice_videoRtc_namesTheUpstreamFileAndProject', () => {
    expect(notice).toContain('video-rtc.js');
    expect(notice).toContain('https://github.com/AlexxIT/go2rtc');
  });

  it('notice_hlsjs_carriesItsApacheNotice', () => {
    expect(notice).toContain('hls.js');
    expect(notice).toContain('Apache License 2.0');
    expect(notice).toContain('Copyright (c) 2017 Dailymotion');
  });
});
