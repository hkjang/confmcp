package com.hkjang.confmcp.permission;

import org.junit.Test;

import java.util.Map;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class PermissionEvaluatorTest {

    @Test
    public void parsesOnlyPlainPositiveIds() {
        assertEquals(65538L, PermissionEvaluator.parseId("65538"));
        assertEquals(-1L, PermissionEvaluator.parseId(""));
        assertEquals(-1L, PermissionEvaluator.parseId("-5"));
        assertEquals(-1L, PermissionEvaluator.parseId("12a"));
        assertEquals(-1L, PermissionEvaluator.parseId("99999999999999999999"));
    }

    @Test
    public void knowsTheContractOperations() {
        for (String op : new String[]{"read", "create", "edit", "comment", "attach", "move", "delete"}) {
            assertTrue(op, PermissionEvaluator.isKnownOp(op));
        }
        assertFalse(PermissionEvaluator.isKnownOp("admin"));
        assertFalse(PermissionEvaluator.isKnownOp("READ"));
    }

    @Test
    public void echoesTargetAsSent() {
        Map<String, Object> content = new PermissionEvaluator.Target("content", null, "att12345").toJson();
        assertEquals("{kind=content, id=att12345}", content.toString());
        Map<String, Object> space = new PermissionEvaluator.Target("space", "doc", "").toJson();
        assertEquals("{kind=space, spaceKey=doc}", space.toString());
    }
}
