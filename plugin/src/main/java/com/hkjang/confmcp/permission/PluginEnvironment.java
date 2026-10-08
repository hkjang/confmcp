package com.hkjang.confmcp.permission;

import com.atlassian.plugin.spring.scanner.annotation.imports.ComponentImport;
import com.atlassian.sal.api.ApplicationProperties;
import com.atlassian.sal.api.transaction.TransactionCallback;
import com.atlassian.sal.api.transaction.TransactionTemplate;
import com.atlassian.sal.api.user.UserKey;
import com.atlassian.sal.api.user.UserManager;

import javax.inject.Inject;
import javax.inject.Named;
import java.io.IOException;
import java.io.InputStream;
import java.util.Properties;

/** Product facts, the current HTTP user and transactions, behind one small bean. */
@Named("confmcpPluginEnvironment")
public class PluginEnvironment {

    private final ApplicationProperties applicationProperties;
    private final UserManager userManager;
    private final TransactionTemplate transactionTemplate;
    private final String pluginVersion;

    @Inject
    public PluginEnvironment(@ComponentImport ApplicationProperties applicationProperties,
                             @ComponentImport UserManager userManager,
                             @ComponentImport TransactionTemplate transactionTemplate) {
        this.applicationProperties = applicationProperties;
        this.userManager = userManager;
        this.transactionTemplate = transactionTemplate;
        this.pluginVersion = readPluginVersion();
    }

    public String pluginVersion() {
        return pluginVersion;
    }

    public String confluenceVersion() {
        return applicationProperties.getVersion();
    }

    public String buildNumber() {
        return applicationProperties.getBuildNumber();
    }

    /** The user logged in to this HTTP request (session or basic auth), or null. */
    public UserKey remoteUserKey() {
        return userManager.getRemoteUserKey();
    }

    public boolean isSystemAdmin(UserKey key) {
        return key != null && userManager.isSystemAdmin(key);
    }

    public <T> T inTransaction(TransactionCallback<T> callback) {
        return transactionTemplate.execute(callback);
    }

    private static String readPluginVersion() {
        InputStream in = PluginEnvironment.class.getResourceAsStream("/confmcp-plugin.properties");
        if (in == null) {
            return "unknown";
        }
        try {
            Properties p = new Properties();
            p.load(in);
            String v = p.getProperty("plugin.version", "unknown").trim();
            return v.isEmpty() || v.startsWith("${") ? "unknown" : v;
        } catch (IOException e) {
            return "unknown";
        } finally {
            try {
                in.close();
            } catch (IOException ignored) {
                // nothing to do
            }
        }
    }
}
