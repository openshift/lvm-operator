# Using Loop Devices with LVMS

Loop devices can be used with LVMS for testing and development purposes. Loop devices allow you to create block devices from regular files, which is useful when you don't have physical disks available or want to test LVMS functionality in a controlled environment.

## Creating and Using Loop Devices

1. Create a file to use as a backing store for the loop device:

   ```bash
   # Create a 10GB file
   dd if=/dev/zero of=/path/to/lvms-test-disk.img bs=1M count=10240
   ```

2. Check for available loop devices:

   ```bash
   # Find the next available loop device
   losetup -f
   ```

3. Set up a loop device using the file:

   ```bash
   # Create a loop device (replace loop_device with the available device from step 2)
   losetup /dev/loop_device /path/to/lvms-test-disk.img
   ```

4. Verify the loop device is created:

   ```bash
   # Check if the loop device exists
   lsblk /dev/loop_device
   ```

5. Use the loop device in your `LVMCluster` by specifying it in the `deviceSelector`:

   ```yaml
   apiVersion: lvm.topolvm.io/v1alpha1
   kind: LVMCluster
   metadata:
     name: my-lvmcluster
   spec:
     storage:
       deviceClasses:
         - name: vg1
           deviceSelector:
             paths:
               - /dev/loop_device
   ```

## Important Considerations

- **Performance**: Loop devices are significantly slower than physical disks and should not be used in production environments.
- **Persistence**: Loop devices created manually are not persistent across reboots. You'll need to recreate them after system restarts.
- **Kubernetes Conflicts**: Loop devices that are already in use by Kubernetes should not be used with LVMS. See [known-limitations.md](known-limitations.md) for the full list of unsupported device types.
- **File Size**: The backing file size determines the maximum capacity available for the loop device. Ensure the file is large enough for your testing needs.

## Cleaning Up Loop Devices

When you're done testing, clean up the loop devices:

```bash
# Detach the loop device
losetup -d /dev/loop_device

# Remove the backing file
rm /path/to/lvms-test-disk.img
```
