package com.hkjang.confmcp.permission;

import com.atlassian.confluence.user.ConfluenceUser;
import com.atlassian.confluence.user.UserAccessor;
import com.atlassian.crowd.embedded.api.CrowdDirectoryService;
import com.atlassian.crowd.embedded.api.CrowdService;
import com.atlassian.crowd.embedded.api.Directory;
import com.atlassian.crowd.embedded.api.User;
import com.atlassian.plugin.spring.scanner.annotation.imports.ComponentImport;
import com.atlassian.sal.api.user.UserKey;
import com.atlassian.spring.container.ContainerManager;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.inject.Inject;
import javax.inject.Named;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** Resolves Confluence accounts and their status for the gateway. */
@Named("confmcpUserLookup")
public class UserLookup {

    private static final Logger log = LoggerFactory.getLogger(UserLookup.class);

    private final UserAccessor userAccessor;
    private final CrowdService crowdService;
    private final CrowdDirectoryService crowdDirectoryService;

    @Inject
    public UserLookup(@ComponentImport UserAccessor userAccessor,
                      @ComponentImport CrowdService crowdService,
                      @ComponentImport CrowdDirectoryService crowdDirectoryService) {
        this.userAccessor = userAccessor;
        this.crowdService = crowdService;
        this.crowdDirectoryService = crowdDirectoryService;
    }

    public ConfluenceUser byKey(String userKey) {
        if (userKey == null || userKey.trim().isEmpty()) {
            return null;
        }
        return userAccessor.getUserByKey(new UserKey(userKey.trim()));
    }

    public ConfluenceUser byName(String username) {
        if (username == null || username.trim().isEmpty()) {
            return null;
        }
        return userAccessor.getUserByName(username.trim());
    }

    public boolean isDeactivated(ConfluenceUser user) {
        return userAccessor.isDeactivated(user);
    }

    public String status(ConfluenceUser user) {
        return isDeactivated(user) ? "deactivated" : "active";
    }

    /** The /users answer for one account. */
    public Map<String, Object> describe(ConfluenceUser user) {
        Map<String, Object> out = new LinkedHashMap<String, Object>();
        out.put("userKey", user.getKey().getStringValue());
        out.put("username", user.getName());
        out.put("displayName", nullToEmpty(user.getFullName()));
        out.put("email", nullToEmpty(user.getEmail()));
        out.put("status", status(user));
        out.put("directory", directoryName(user.getName()));
        out.put("duplicates", duplicates(user.getName()));
        return out;
    }

    private String directoryName(String username) {
        try {
            User crowdUser = crowdService.getUser(username);
            if (crowdUser == null) {
                return "";
            }
            Directory dir = crowdDirectoryService.findDirectoryById(crowdUser.getDirectoryId());
            return dir == null ? "" : nullToEmpty(dir.getName());
        } catch (RuntimeException e) {
            log.debug("confmcp: directory lookup failed: {}", e.toString());
            return "";
        }
    }

    /**
     * How many active user directories contain this username. Confluence only
     * exposes the winning (first) directory through its public API, so this
     * asks Crowd's DirectoryManager reflectively; if that is unavailable the
     * answer is 1, as the gateway contract allows.
     */
    private int duplicates(String username) {
        try {
            Object directoryManager = ContainerManager.getComponent("crowdDirectoryManager");
            if (directoryManager == null) {
                return 1;
            }
            Method find = directoryManager.getClass().getMethod("findUserByName", long.class, String.class);
            List<Directory> directories = crowdDirectoryService.findAllDirectories();
            int count = 0;
            for (Directory dir : directories) {
                if (!dir.isActive()) {
                    continue;
                }
                try {
                    if (find.invoke(directoryManager, dir.getId(), username) != null) {
                        count++;
                    }
                } catch (InvocationTargetException notFound) {
                    // UserNotFoundException / DirectoryNotFoundException: not in this directory.
                }
            }
            return Math.max(count, 1);
        } catch (Exception e) {
            log.debug("confmcp: duplicate-directory count unavailable: {}", e.toString());
            return 1;
        } catch (LinkageError e) {
            log.debug("confmcp: duplicate-directory count unavailable: {}", e.toString());
            return 1;
        }
    }

    private static String nullToEmpty(String s) {
        return s == null ? "" : s;
    }
}
