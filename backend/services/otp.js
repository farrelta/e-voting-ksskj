const crypto = require("crypto");
const redisClient = require("../redis");

const OTP_TTL_SECONDS = 5 * 60;
const OTP_MAX_ATTEMPTS = 5;
const OTP_RESEND_COOLDOWN = 30;

function generateOtp() {
    return crypto
        .randomInt(0, 1000000)
        .toString()
        .padStart(6, "0");
}

function hashOtp(otp) {
    return crypto
        .createHmac("sha256", process.env.OTP_SECRET)
        .update(otp)
        .digest("hex");
}

function safeCompare(a, b) {
    const aBuffer = Buffer.from(a, "hex");
    const bBuffer = Buffer.from(b, "hex");

    if (aBuffer.length !== bBuffer.length) {
        return false;
    }

    return crypto.timingSafeEqual(aBuffer, bBuffer);
}

async function createOtpChallenge(userId) {
    const activeKey = `otp:active:${userId}`;

    // Invalidate any previous OTP challenge
    const previousChallengeId =
        await redisClient.get(activeKey);

    if (previousChallengeId) {
        await redisClient.del(
            `otp:login:${previousChallengeId}`,
            `otp:attempts:${previousChallengeId}`,
            `otp:resend:${previousChallengeId}`
        );
    }

    const challengeId = crypto.randomUUID();
    const otp = generateOtp();
    const otpHash = hashOtp(otp);

    await redisClient.set(
        `otp:login:${challengeId}`,
        JSON.stringify({
            userId,
            otpHash,
            createdAt: Date.now()
        }),
        {
            EX: OTP_TTL_SECONDS
        }
    );

    await redisClient.set(
        activeKey,
        challengeId,
        {
            EX: OTP_TTL_SECONDS
        }
    );

    return {
        challengeId,
        otp
    };
}

async function verifyOtpChallenge(challengeId, submittedOtp) {
    const key = `otp:login:${challengeId}`;
    const attemptsKey = `otp:attempts:${challengeId}`;

    const raw = await redisClient.get(key);

    if (!raw) {
        return {
            success: false,
            reason: "expired_or_invalid"
        };
    }

    const challenge = JSON.parse(raw);

    const attempts = await redisClient.incr(attemptsKey);

    if (attempts === 1) {
        await redisClient.expire(
            attemptsKey,
            OTP_TTL_SECONDS
        );
    }

    if (attempts >= OTP_MAX_ATTEMPTS) {
        await redisClient.del(key, attemptsKey, `otp:active:${challenge.userId}`);

        return {
            success: false,
            reason: "too_many_attempts"
        };
    }

    const submittedHash = hashOtp(submittedOtp);

    if (!safeCompare(submittedHash, challenge.otpHash)) {
        return {
            success: false,
            reason: "invalid_otp"
        };
    }

    await redisClient.del(key, attemptsKey, `otp:active:${challenge.userId}`);

    return {
        success: true,
        userId: challenge.userId
    };
}

async function canResendOtp(challengeId) {
    const cooldownKey = `otp:resend:${challengeId}`;

    const exists = await redisClient.exists(cooldownKey);

    if (exists) {
        return false;
    }

    await redisClient.set(
        cooldownKey,
        "1",
        {
            EX: OTP_RESEND_COOLDOWN
        }
    );

    return true;
}

async function resendOtp(challengeId) {
    const key = `otp:login:${challengeId}`;

    const raw = await redisClient.get(key);

    if (!raw) {
        return null;
    }

    const challenge = JSON.parse(raw);

    const otp = generateOtp();
    const otpHash = hashOtp(otp);

    await redisClient.set(
        key,
        JSON.stringify({
            userId: challenge.userId,
            otpHash,
            createdAt: Date.now()
        }),
        {
            EX: OTP_TTL_SECONDS
        }
    );

    await redisClient.del(
        `otp:attempts:${challengeId}`,
        `otp:active:${challenge.userId}`
    );

    return {
        otp
    };
}

module.exports = {
    createOtpChallenge,
    verifyOtpChallenge,
    canResendOtp,
    resendOtp
};
