# VHS tape for verda vm create wizard demo
# https://github.com/charmbracelet/vhs
#
# Target selections:
#   Billing: On-Demand / Pay as you go
#   Compute: GPU / 1RTXPRO6000.30V
#   Location: FIN-00
#   Image: 2404f580-1300-1111-82b3-f199fb9b701b
#   Volume: 50 GiB (default)
#   SSH Key: cace0fae-a43d-40d4-94bd-07178102923e
#   Hostname: early-funky-opossum-fin-00

Output demo.gif

Set FontSize 14
Set Width 1200
Set Height 800
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms

# Start the command
Type "verda vm create"
Enter
Sleep 2s

# Step 1: Billing Type — select "On-Demand" (first item)
Enter
Sleep 1s

# Step 2: Contract Period — select "Pay as you go" (first item)
Enter
Sleep 1s

# Step 3: Compute Type — select "GPU" (first item)
Enter
Sleep 3s  # wait for API to load instance types

# Step 4: Instance Type — navigate to 1RTXPRO6000.30V
# TODO: adjust Down count to match position in your list
Down
Down
Enter
Sleep 3s  # wait for API to load locations

# Step 5: Datacenter Location — navigate to FIN-00
# TODO: adjust Down count to match position in your list
Enter
Sleep 3s  # wait for API to load OS images

# Step 6: OS Image — navigate to target image
# TODO: adjust Down count to match position in your list
Enter
Sleep 1s

# Step 7: OS Volume Size — accept default 50 GiB
Enter
Sleep 2s

# Step 8: Storage Volumes — skip (None)
Enter
Sleep 2s

# Step 9: SSH Keys — select key then confirm
# TODO: adjust Down count to reach your SSH key
Down
Space
Enter
Sleep 2s

# Step 10: Startup Script — skip (None)
Enter
Sleep 1s

# Step 11: Hostname — accept auto-generated default
Enter
Sleep 1s

# Step 12: Description — accept default
Enter
Sleep 2s

# Step 13: Deploy Confirmation
Type "y"
Enter
Sleep 5s
