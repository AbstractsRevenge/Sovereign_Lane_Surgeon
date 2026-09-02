/*
 * Copyright (C) 2024 The Android Open Source Project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package com.android.test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.junit.Assume.assumeTrue;
import org.junit.Before;
import org.junit.After;

import android.platform.test.annotations.AppModeFull;

import com.android.tradefed.device.DeviceNotAvailableException;
import com.android.tradefed.testtype.DeviceJUnit4ClassRunner;
import com.android.tradefed.testtype.junit4.BaseHostJUnit4Test;
import com.android.tradefed.testtype.junit4.DeviceTestRunOptions;
import com.android.tradefed.util.CommandResult;
import com.android.tradefed.util.RunUtil;

import org.junit.Test;
import org.junit.runner.RunWith;

import java.io.BufferedReader;
import java.io.StringReader;

@RunWith(DeviceJUnit4ClassRunner.class)
public class CopyEfsTest extends BaseHostJUnit4Test {

    @Before
    public void setUp() throws Exception {
        getDevice().enableAdbRoot();

        getDevice().executeShellCommand("rm -rf /data/local/tmp/efs_test");
        getDevice().executeShellCommand("mkdir -p /data/local/tmp/efs_test/mnt");
        getDevice().executeShellCommand("mkdir -p /data/local/tmp/efs_test/dump");
    }

    @Test
    @AppModeFull
    public void copyEfsTest() throws Exception {
        assumeTrue(getDevice().executeShellCommand("getconf PAGESIZE").trim().equals("4096"));

        testDumpF2FS("efs");
        testDumpF2FS("efs_backup");
        testDumpF2FS("modem_userdata");
        testDumpF2FS("persist");
    }

    private CommandResult RunAndCheckAdbCmd(String cmd) throws DeviceNotAvailableException {
        CommandResult r = getDevice().executeShellV2Command(cmd);
        assertEquals("Failed to run " + cmd, Integer.valueOf(0), r.getExitCode());
        return r;
    }

    // Remove timestamps because ls on device does not support --time-style.
    // Format is [permissions] [links] [uid] [gid] [size] time [name/symlink]
    // time may vary greatly in formatting
    // symlinks will be of the form a -> b
    // So we can check for -> in the second to last spot to determine what position the timestamp ends at
    // Remove totals because on disk block usage may change depending on filesystem
    private String removeTimestamps(String input) {
        StringBuilder output = new StringBuilder();
        for (String line : input.split("\n")) {
            String[] tokens = line.split("(?<![\\\\])\s+");
            if (tokens[0].equals("total"))
                continue;
            if (tokens.length < 3) {
                output.append(line + "\n");
                continue;
            }
            int name_offset = 1;
            if (tokens[tokens.length - 2].equals("->"))
                name_offset = 3;
            for (int i=0; i<tokens.length; i++) {
                if (i >= 5 && i < tokens.length - name_offset)
                    continue;
                if (i != 0)
                    output.append(" ");
                output.append(tokens[i]);
            }
            output.append("\n");
        }
        return output.toString();
    }

    private void testDumpF2FS(String name) throws Exception {
        RunAndCheckAdbCmd(String.format("cp /dev/block/by-name/%s /data/local/tmp/efs_test/%s.img", name, name));

        // The device was mounted r/w. To get a clean image, we run fsck, and then mount to allow mount time fixes to happen.
        // We can then dump and mount read only to ensure the contents should be the same.
        RunAndCheckAdbCmd(String.format("fsck.f2fs -f /data/local/tmp/efs_test/%s.img", name));
        RunAndCheckAdbCmd(String.format("mount /data/local/tmp/efs_test/%s.img /data/local/tmp/efs_test/mnt", name));
        RunAndCheckAdbCmd("umount /data/local/tmp/efs_test/mnt");

        RunAndCheckAdbCmd(String.format("dump.f2fs -rfPLo /data/local/tmp/efs_test/dump /data/local/tmp/efs_test/%s.img", name));
        RunAndCheckAdbCmd(String.format("mount -r /data/local/tmp/efs_test/%s.img /data/local/tmp/efs_test/mnt", name));

        CommandResult r = RunAndCheckAdbCmd("diff -rq --no-dereference /data/local/tmp/efs_test/mnt /data/local/tmp/efs_test/dump");
        assertEquals(r.getStdout(), "");

        String ls_cmd = "cd /data/local/tmp/efs_test/%s;ls -AlnR .";
        CommandResult mnt_ls = RunAndCheckAdbCmd(String.format(ls_cmd, "mnt"));
        CommandResult dump_ls = RunAndCheckAdbCmd(String.format(ls_cmd, "dump"));
        assertEquals(removeTimestamps(mnt_ls.getStdout()), removeTimestamps(dump_ls.getStdout()));

        getDevice().executeShellCommand("umount /data/local/tmp/efs_test/mnt");
        getDevice().executeShellCommand("rm -rf /data/local/tmp/efs_test/dump/*");
        getDevice().executeShellCommand("rm /data/local/tmp/efs_test/" + name + ".img");
    }

    @After
    public void tearDown() throws Exception {
        getDevice().executeShellCommand("umount /data/local/tmp/efs_test/mnt");
        getDevice().executeShellCommand("rm -rf /data/local/tmp/efs_test");
    }
}
