package com.hkjang.confmcp.permission;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;

/**
 * Verifies the confmcp gateway's request signature.
 *
 * <pre>
 * signature = hex(HMAC-SHA256(secret,
 *     METHOD + "\n" + escapedRequestUri + "\n" + hex(sha256(body)) + "\n" + timestamp + "\n" + nonce))
 * </pre>
 *
 * The request URI is the raw, still-escaped path the servlet container saw,
 * context path included (for example {@code /confluence/rest/confmcp/1.0/health}).
 * This is byte-for-byte what {@code internal/permission.Sign} in the Go gateway
 * computes over {@code url.EscapedPath()}.
 *
 * A timestamp more than five minutes away from the server clock is rejected,
 * and every nonce is remembered for ten minutes so a captured request cannot
 * be replayed. Nonces are only remembered after the signature has been
 * verified, so unsigned traffic cannot fill the cache.
 *
 * Pure Java on purpose: no Atlassian types, so it is unit-testable as is.
 */
public final class SignatureVerifier {

    public static final String HEADER_TIMESTAMP = "X-Confmcp-Timestamp";
    public static final String HEADER_NONCE = "X-Confmcp-Nonce";
    public static final String HEADER_SIGNATURE = "X-Confmcp-Signature";

    public static final long MAX_SKEW_SECONDS = 300L;
    public static final long NONCE_TTL_SECONDS = 600L;
    public static final int DEFAULT_MAX_NONCES = 100000;
    static final int MAX_NONCE_LENGTH = 128;

    /** Outcome of a verification. Only {@link #OK} lets a request through. */
    public enum Result {
        OK("ok"),
        MISSING_HEADERS("missing signature headers"),
        BAD_TIMESTAMP("malformed timestamp"),
        STALE_TIMESTAMP("timestamp outside the allowed window"),
        BAD_NONCE("malformed nonce"),
        BAD_SIGNATURE("invalid gateway signature"),
        REPLAYED_NONCE("nonce already used"),
        REPLAY_CACHE_FULL("replay cache full, retry later");

        private final String message;

        Result(String message) {
            this.message = message;
        }

        public String message() {
            return message;
        }
    }

    /** Seconds since the epoch; replaceable for tests. */
    public interface Clock {
        long nowSeconds();
    }

    private static final Clock SYSTEM_CLOCK = new Clock() {
        @Override
        public long nowSeconds() {
            return System.currentTimeMillis() / 1000L;
        }
    };

    private final Clock clock;
    private final int maxNonces;
    /** nonce -> expiry (epoch seconds); insertion order is expiry order. */
    private final LinkedHashMap<String, Long> nonces = new LinkedHashMap<String, Long>();

    public SignatureVerifier() {
        this(SYSTEM_CLOCK, DEFAULT_MAX_NONCES);
    }

    public SignatureVerifier(Clock clock, int maxNonces) {
        if (clock == null) {
            throw new IllegalArgumentException("clock");
        }
        if (maxNonces < 1) {
            throw new IllegalArgumentException("maxNonces");
        }
        this.clock = clock;
        this.maxNonces = maxNonces;
    }

    /**
     * Checks one request.
     *
     * @param secret     shared secret, must not be empty
     * @param method     HTTP method
     * @param requestUri escaped request URI including the context path, without query string
     * @param body       raw body bytes (empty or null for GET)
     */
    public Result verify(String secret, String method, String requestUri, byte[] body,
                         String timestamp, String nonce, String signature) {
        if (secret == null || secret.isEmpty()) {
            throw new IllegalArgumentException("secret not configured");
        }
        if (isBlank(timestamp) || isBlank(nonce) || isBlank(signature) || method == null || requestUri == null) {
            return Result.MISSING_HEADERS;
        }
        String ts = timestamp.trim();
        long sent;
        try {
            sent = Long.parseLong(ts);
        } catch (NumberFormatException e) {
            return Result.BAD_TIMESTAMP;
        }
        long now = clock.nowSeconds();
        if (Math.abs(now - sent) > MAX_SKEW_SECONDS) {
            return Result.STALE_TIMESTAMP;
        }
        String n = nonce.trim();
        if (n.length() > MAX_NONCE_LENGTH || !isPrintableAscii(n)) {
            return Result.BAD_NONCE;
        }

        String expected = sign(secret, method, requestUri, body, ts, n);
        String presented = signature.trim().toLowerCase(Locale.ROOT);
        if (!MessageDigest.isEqual(expected.getBytes(StandardCharsets.US_ASCII),
                presented.getBytes(StandardCharsets.US_ASCII))) {
            return Result.BAD_SIGNATURE;
        }
        return remember(n, now);
    }

    private synchronized Result remember(String nonce, long now) {
        purge(now);
        if (nonces.containsKey(nonce)) {
            return Result.REPLAYED_NONCE;
        }
        if (nonces.size() >= maxNonces) {
            // Fail closed: forgetting a live nonce early would reopen replay.
            return Result.REPLAY_CACHE_FULL;
        }
        nonces.put(nonce, now + NONCE_TTL_SECONDS);
        return Result.OK;
    }

    private void purge(long now) {
        Iterator<Map.Entry<String, Long>> it = nonces.entrySet().iterator();
        while (it.hasNext()) {
            if (it.next().getValue() > now) {
                break;
            }
            it.remove();
        }
    }

    synchronized int rememberedNonces() {
        return nonces.size();
    }

    /** Computes the signature exactly as the Go gateway does. */
    public static String sign(String secret, String method, String requestUri, byte[] body,
                              String timestamp, String nonce) {
        byte[] payload = (method.toUpperCase(Locale.ROOT) + "\n" + requestUri + "\n"
                + hex(sha256(body == null ? new byte[0] : body)) + "\n" + timestamp + "\n" + nonce)
                .getBytes(StandardCharsets.UTF_8);
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
            return hex(mac.doFinal(payload));
        } catch (GeneralSecurityException e) {
            throw new IllegalStateException("HmacSHA256 unavailable", e);
        }
    }

    static byte[] sha256(byte[] data) {
        try {
            return MessageDigest.getInstance("SHA-256").digest(data);
        } catch (GeneralSecurityException e) {
            throw new IllegalStateException("SHA-256 unavailable", e);
        }
    }

    static String hex(byte[] data) {
        char[] out = new char[data.length * 2];
        for (int i = 0; i < data.length; i++) {
            out[2 * i] = Character.forDigit((data[i] >> 4) & 0xF, 16);
            out[2 * i + 1] = Character.forDigit(data[i] & 0xF, 16);
        }
        return new String(out);
    }

    private static boolean isBlank(String s) {
        return s == null || s.trim().isEmpty();
    }

    private static boolean isPrintableAscii(String s) {
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c < 0x21 || c > 0x7E) {
                return false;
            }
        }
        return true;
    }
}
