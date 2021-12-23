# Cleanup design

Cleanup: Operations needed to do when the lvm cluster CRD is removed.

## Main requirements

Remove all the operator resource units and associated resources (config files, socket files). 
This part is mostly complete and implemented in the ensureDeleted methods of the different resource units.

In this document our target is to decide how to remove all the VGs created because operator working behaviour. 

Main requirements are:
- Volume Groups created by the operator must only be removed if we do not have PVCs or PVs using them.
- If we decide that a VG need to be deleted, we must delete the VGs and also the Physical Volume that supports the VG. 
- Any solution mus be compatible with the "future". (Multiples nodes, multiple lvm cluster CRDs) 

## How to implement the cleanup

1. **Use a "customized" CRD for VG manager. (VG manager CR)**

- lvm controller creates a new CR that is the one Vg manager watches. 
we copy the information VG manager will need from the lvm cluster CR. 
- When the lvm clsuter Cr is deleted, the lvm controller will check if it is possible to delete the storage classes
if that is possible then the topolvm cluster controller and topolvm_node resource units are deleted. 
- Reached this point. (dynamic storage requests not possible), the lvm op. controller deletes the "VG manager CR".
- The "VR manager CR" has a finalizer set by the VG manager (problem to solve, multiple VG managers watching same CRD)
- The VG manager reconcile loop will find that the Cr is deleted, and we can launch the VG deletion in all the nodes and clear the finalizer
- Back to the lvm operator controller reconcile loop. It detects that "VR manager CRD" has been deleted, then the operator deleted the VR manager daemonset CR

 
2. **Replace lvmd demonset with a job** (TODO : Leela please refine this I didn't understand well your idea)