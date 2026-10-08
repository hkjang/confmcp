package com.hkjang.confmcp.permission;

import org.junit.Before;
import org.junit.Test;

import java.nio.charset.StandardCharsets;

import static org.junit.Assert.assertEquals;

public class SignatureVerifierTest {

    private static final String SECRET = "s3cr3t-confmcp";
    private static final String BATCH_PATH = "/confluence/rest/confmcp/1.0/permissions/batch";
    private static final byte[] BATCH_BODY = ("{\"userKey\":\"8a7f808a6f1e2b3c016f1e2b5a0a0001\",\"checks\":[{\"target\":"
            + "{\"kind\":\"content\",\"id\":\"att12345\"},\"operations\":[\"read\",\"edit\"]}],\"correlationId\":\"c-1\"}")
            .getBytes(StandardCharsets.UTF_8);
    private static final String BATCH_TS = "1700000000";
    private static final String BATCH_NONCE = "0123456789abcdef0123456789abcdef";

    // Expected values computed with the Go gateway's permission.Sign.
    private static final String GO_BATCH_SIG = "be00d7c97fac5975f67968d2f38653447be8a4d792accf117348e8bd26e46de3";
    private static final String GO_HEALTH_SIG = "48b3e49ef2b9699dfbeed71d0d3d846ca9bbb4238acd938be4a55d2174eba9fc";
    private static final String GO_UTF8_SIG = "7b267b6b1a37a36eec04bc85c4785323421f3b9afcb978ea97293b04a659e1a3";

    private long now;
    private SignatureVerifier verifier;

    @Before
    public void setUp() {
        now = 1700000000L;
        verifier = new SignatureVerifier(new SignatureVerifier.Clock() {
            @Override
            public long nowSeconds() {
                return now;
            }
        }, 3);
    }

    @Test
    public void signMatchesGoVectorForPost() {
        assertEquals(GO_BATCH_SIG, SignatureVerifier.sign(SECRET, "POST", BATCH_PATH, BATCH_BODY, BATCH_TS, BATCH_NONCE));
    }

    @Test
    public void signMatchesGoVectorForGetWithEmptyBody() {
        assertEquals(GO_HEALTH_SIG, SignatureVerifier.sign(SECRET, "GET", "/rest/confmcp/1.0/health", new byte[0], "1700000000", "nonce-1"));
        assertEquals(GO_HEALTH_SIG, SignatureVerifier.sign(SECRET, "get", "/rest/confmcp/1.0/health", null, "1700000000", "nonce-1"));
    }

    @Test
    public void signMatchesGoVectorForUtf8SecretBodyAndEscapedPath() {
        assertEquals(GO_UTF8_SIG, SignatureVerifier.sign("비밀값", "POST", "/wiki%20x/rest/confmcp/1.0/users",
                "{\"username\":\"홍길동\"}".getBytes(StandardCharsets.UTF_8), "1700000123", "n2"));
    }

    @Test
    public void acceptsValidSignatureOnce() {
        assertEquals(SignatureVerifier.Result.OK, verifyBatch(GO_BATCH_SIG));
        assertEquals(SignatureVerifier.Result.REPLAYED_NONCE, verifyBatch(GO_BATCH_SIG));
    }

    @Test
    public void acceptsUppercaseHexSignature() {
        assertEquals(SignatureVerifier.Result.OK, verifyBatch(GO_BATCH_SIG.toUpperCase()));
    }

    @Test
    public void rejectsTamperedBodyPathMethodAndSecret() {
        byte[] tampered = BATCH_BODY.clone();
        tampered[12] = 'X';
        assertEquals(SignatureVerifier.Result.BAD_SIGNATURE,
                verifier.verify(SECRET, "POST", BATCH_PATH, tampered, BATCH_TS, BATCH_NONCE, GO_BATCH_SIG));
        assertEquals(SignatureVerifier.Result.BAD_SIGNATURE,
                verifier.verify(SECRET, "POST", "/rest/confmcp/1.0/permissions/batch", BATCH_BODY, BATCH_TS, BATCH_NONCE, GO_BATCH_SIG));
        assertEquals(SignatureVerifier.Result.BAD_SIGNATURE,
                verifier.verify(SECRET, "PUT", BATCH_PATH, BATCH_BODY, BATCH_TS, BATCH_NONCE, GO_BATCH_SIG));
        assertEquals(SignatureVerifier.Result.BAD_SIGNATURE,
                verifier.verify("other", "POST", BATCH_PATH, BATCH_BODY, BATCH_TS, BATCH_NONCE, GO_BATCH_SIG));
        assertEquals(SignatureVerifier.Result.BAD_SIGNATURE,
                verifier.verify(SECRET, "POST", BATCH_PATH, BATCH_BODY, BATCH_TS, BATCH_NONCE, GO_BATCH_SIG.substring(2)));
        // A failed attempt must not burn the nonce for the genuine request.
        assertEquals(SignatureVerifier.Result.OK, verifyBatch(GO_BATCH_SIG));
    }

    @Test
    public void enforcesFiveMinuteWindow() {
        now = 1700000000L + 300;
        assertEquals(SignatureVerifier.Result.OK, verifyBatch(GO_BATCH_SIG));

        setUp();
        now = 1700000000L + 301;
        assertEquals(SignatureVerifier.Result.STALE_TIMESTAMP, verifyBatch(GO_BATCH_SIG));
        now = 1700000000L - 301;
        assertEquals(SignatureVerifier.Result.STALE_TIMESTAMP, verifyBatch(GO_BATCH_SIG));
        now = 1700000000L - 300;
        assertEquals(SignatureVerifier.Result.OK, verifyBatch(GO_BATCH_SIG));
    }

    @Test
    public void rejectsMissingOrMalformedHeaders() {
        assertEquals(SignatureVerifier.Result.MISSING_HEADERS,
                verifier.verify(SECRET, "POST", BATCH_PATH, BATCH_BODY, null, BATCH_NONCE, GO_BATCH_SIG));
        assertEquals(SignatureVerifier.Result.MISSING_HEADERS,
                verifier.verify(SECRET, "POST", BATCH_PATH, BATCH_BODY, BATCH_TS, "", GO_BATCH_SIG));
        assertEquals(SignatureVerifier.Result.MISSING_HEADERS,
                verifier.verify(SECRET, "POST", BATCH_PATH, BATCH_BODY, BATCH_TS, BATCH_NONCE, " "));
        assertEquals(SignatureVerifier.Result.BAD_TIMESTAMP,
                verifier.verify(SECRET, "POST", BATCH_PATH, BATCH_BODY, "17e8", BATCH_NONCE, GO_BATCH_SIG));
        assertEquals(SignatureVerifier.Result.BAD_NONCE,
                verifier.verify(SECRET, "POST", BATCH_PATH, BATCH_BODY, BATCH_TS, "bad nonce", GO_BATCH_SIG));
    }

    @Test
    public void nonceIsForgottenAfterTenMinutes() {
        assertEquals(SignatureVerifier.Result.OK, signedHealth("n-1"));
        now += 599;
        assertEquals(SignatureVerifier.Result.REPLAYED_NONCE, signedHealth("n-1"));
        now += 1;
        assertEquals(SignatureVerifier.Result.OK, signedHealth("n-1"));
    }

    @Test
    public void failsClosedWhenReplayCacheIsFull() {
        assertEquals(SignatureVerifier.Result.OK, signedHealth("a"));
        assertEquals(SignatureVerifier.Result.OK, signedHealth("b"));
        assertEquals(SignatureVerifier.Result.OK, signedHealth("c"));
        assertEquals(SignatureVerifier.Result.REPLAY_CACHE_FULL, signedHealth("d"));
        now += SignatureVerifier.NONCE_TTL_SECONDS;
        assertEquals(SignatureVerifier.Result.OK, signedHealth("d"));
        assertEquals(1, verifier.rememberedNonces());
    }

    @Test(expected = IllegalArgumentException.class)
    public void refusesEmptySecret() {
        verifier.verify("", "GET", "/rest/confmcp/1.0/health", null, BATCH_TS, "n", "00");
    }

    private SignatureVerifier.Result verifyBatch(String sig) {
        return verifier.verify(SECRET, "POST", BATCH_PATH, BATCH_BODY, BATCH_TS, BATCH_NONCE, sig);
    }

    private SignatureVerifier.Result signedHealth(String nonce) {
        String ts = Long.toString(now);
        String sig = SignatureVerifier.sign(SECRET, "GET", "/rest/confmcp/1.0/health", null, ts, nonce);
        return verifier.verify(SECRET, "GET", "/rest/confmcp/1.0/health", null, ts, nonce, sig);
    }
}
