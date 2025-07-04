// Copyright (C) 2022 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import uuid
import system.containers
import system.api.containers show ContainerService
import system.storage

JAGUAR-INSTALLED-MAGIC ::= 0xb16_ca7  // Magic is "big cat".

flash_ / storage.Bucket ::= storage.Bucket.open --flash "toitlang.org/jag"
jaguar_ / uuid.Uuid ::= containers.current

class ContainerRegistry:
  static KEY_ /string ::= "containers"

  id-by-name_         / Map ::= {:}  // Map<string, uuid.Uuid>
  name-by-id_         / Map ::= {:}  // Map<uuid.Uuid, string>
  entry-by-id-string_ / Map ::= {:}  // Map<string, List>
  /** The current revision of the container. Reset to 0 at every boot. */
  revisions_       / Map ::= {:}  // Map<string, int>

  constructor:
    entries/Map := {:}
    catch: entries = flash_.get KEY_

    // Run through the images actually installed in flash and update the
    // registry accordingly. This involves inventing names for unexpected
    // containers found in flash and pruning names for containers that
    // we cannot find anymore.
    index := 0
    images/List ::= containers.images
    images.do: | image/containers.ContainerImage |
      id ::= image.id
      // Skip transient images that aren't named and installed by Jaguar.
      if id != jaguar_ and image.data != JAGUAR-INSTALLED-MAGIC:
        continue.do
      // We are not sure that the entries loaded from flash is a map
      // with the correct structure, so we guard the access to the
      // individual entries and treat malformed ones as non-existing.
      id-as-string ::= "$id"
      entry ::= entries.get id-as-string
      name/string? := null
      catch: name = entry[0]
      name = name or image.name or "container-$(index++)"
      defines/Map := {:}
      catch: defines = entry[1]
      // Update the in-memory registry mappings.
      id-by-name_[name] = id
      name-by-id_[id] = name
      entry-by-id-string_[id-as-string] = [name, defines, id]
      if name: revisions_[name] = 0

  entries -> Map:
    return entry-by-id-string_.map: | _ entry/List | entry[0]

  do [block] -> none:
    entry-by-id-string_.do: | _ entry/List |
      id ::= entry[2]
      if id == jaguar_: continue.do
      block.call entry[0] id entry[1]

  install name/string? defines/Map [block] -> uuid.Uuid:
    // Uninstall all unnamed images. This is used to prepare
    // for running another unnamed image.
    images/List ::= containers.images
    images.do: | image/containers.ContainerImage |
      id ::= image.id
      if not name-by-id_.contains id: containers.uninstall id
    if name: uninstall name
    // Now actually create the image by invoking the block.
    id ::= block.call
    if not name: return id
    // Update the name mapping and make sure we do not have
    // an old name for the same image floating around.
    old ::= name-by-id_.get id
    if old: id-by-name_.remove old
    id-by-name_[name] = id
    name-by-id_[id] = name
    entry-by-id-string_["$id"] = [name, defines, id]
    store_
    if name: revisions_.update name --if-absent=0: it + 1
    return id

  uninstall name/string -> uuid.Uuid?:
    id := id-by-name_.get name --if-absent=: return null
    containers.uninstall id
    id-by-name_.remove name
    name-by-id_.remove id
    entry-by-id-string_.remove "$id"
    store_
    // We don't remove entries from revisions_ when containers are uninstalled,
    // so that we guarantee that a newer revision of a program still has a newer
    // revision-number, even if it was uninstalled at some point.
    return id

  contains name/string -> bool:
    return id-by-name_.contains name

  get-entry-by-id id/uuid.Uuid -> List?:
    return entry-by-id-string_.get "$id" --if-absent=: null

  revision name/string -> int:
    if name == "": return 0
    return revisions_.get name

  store_ -> none:
    entries := entry-by-id-string_.map: | _ entry/List | entry[0..2]
    flash_[KEY_] = entries
