// TOTP helpers wrapping the `otpauth` library.
// Used by the 2FA wizard and login challenge for Admin / Super Admin.

import { Secret, TOTP } from "otpauth";

const ISSUER = "VozZap";

export type TwoFASetup = {
  secret: string;          // base32 secret
  otpauthUrl: string;      // otpauth://totp/... — used by QR
  recoveryCodes: string[]; // 10 single-use recovery codes
};

/** Generate a fresh TOTP secret + provisioning URL + 10 recovery codes. */
export const generateSetup = (accountEmail: string): TwoFASetup => {
  const secret = new Secret({ size: 20 });
  const totp = new TOTP({
    issuer: ISSUER,
    label: accountEmail,
    algorithm: "SHA1",
    digits: 6,
    period: 30,
    secret,
  });
  const recoveryCodes = Array.from({ length: 10 }, () =>
    Array.from({ length: 2 }, () =>
      Math.random().toString(36).slice(2, 7).toUpperCase(),
    ).join("-"),
  );
  return {
    secret: secret.base32,
    otpauthUrl: totp.toString(),
    recoveryCodes,
  };
};

/** Verify a 6-digit code against a base32 secret (±1 step window). */
export const verifyCode = (secretBase32: string, code: string): boolean => {
  const clean = code.replace(/\D/g, "");
  if (clean.length !== 6) return false;
  try {
    const totp = new TOTP({
      issuer: ISSUER,
      algorithm: "SHA1",
      digits: 6,
      period: 30,
      secret: Secret.fromBase32(secretBase32),
    });
    const delta = totp.validate({ token: clean, window: 1 });
    return delta !== null;
  } catch {
    return false;
  }
};
