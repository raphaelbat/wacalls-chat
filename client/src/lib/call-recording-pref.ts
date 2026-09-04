const KEY = "wacalls.callRecording";

export const isCallRecordingEnabled = (): boolean => {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
};

export const setCallRecordingEnabled = (on: boolean): void => {
  try {
    localStorage.setItem(KEY, on ? "1" : "0");
  } catch {
    /* ignore */
  }
};
