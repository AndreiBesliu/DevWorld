/**
 * Shows a received WebRTC track in the element. Returns a function that detaches it.
 *
 * The track belongs to the session's slot (MediaLink.webrtcTrack), so it is
 * never stopped here, only unplugged from the element.
 */
export function attachTrack(video: HTMLVideoElement, track: MediaStreamTrack): () => void {
  video.srcObject = new MediaStream([track]);
  return () => {
    video.srcObject = null;
  };
}
