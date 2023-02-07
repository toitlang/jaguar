// Copyright (C) 2022 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import uuid
import system.containers
import system.api.containers show ContainerService
import system.storage

JAGUAR_INSTALLED_MAGIC ::= 0xb16_ca7  // Magic is "big cat".

flash_ / storage.Bucket ::= storage.Bucket.open --flash "toitlang.org/jag"
jaguar_ / uuid.Uuid ::= containers.current

class ContainerRegistry:
  static KEY_ /string ::= "containers"

  id_by_name_         / Map ::= {:}  // Map<string, uuid.Uuid>
  name_by_id_         / Map ::= {:}  // Map<uuid.Uuid, string>
  entry_by_id_string_ / Map ::= {:}  // Map<string, List>

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
      if id != jaguar_ and image.data != JAGUAR_INSTALLED_MAGIC:
        continue.do
      // We are not sure that the entries loaded from flash is a map
      // with the correct structure, so we guard the access to the
      // individual entries and treat malformed ones as non-existing.
      id_as_string ::= "$id"
      entry ::= entries.get id_as_string
      name/string? := null
      catch: name = entry[0]
      name = name or image.name or "container-$(index++)"
      defines/Map := {:}
      catch: defines = entry[1]
      // Update the in-memory registry mappings.
      id_by_name_[name] = id
      name_by_id_[id] = name
      entry_by_id_string_[id_as_string] = [name, defines, id]

  entries -> Map:
    return entry_by_id_string_.map: | _ entry/List | entry[0]

  do [block] -> none:
    entry_by_id_string_.do: | _ entry/List |
      id ::= entry[2]
      if id == jaguar_: continue.do
      block.call entry[0] id entry[1]

  install name/string? defines/Map [block] -> uuid.Uuid:
    // Uninstall all unnamed images. This is used to prepare
    // for running another unnamed image.
    images/List ::= containers.images
    images.do: | image/containers.ContainerImage |
      id ::= image.id
      if not name_by_id_.contains id: containers.uninstall id
    if name: uninstall name
    // Now actually create the image by invoking the block.
    id ::= block.call
    if not name: return id
    // Update the name mapping and make sure we do not have
    // an old name for the same image floating around.
    old ::= name_by_id_.get id
    if old: id_by_name_.remove old
    id_by_name_[name] = id
    name_by_id_[id] = name
    entry_by_id_string_["$id"] = [name, defines, id]
    store_
    return id

  uninstall name/string -> uuid.Uuid?:
    id := id_by_name_.get name --if_absent=: return null
    containers.uninstall id
    id_by_name_.remove name
    name_by_id_.remove id
    entry_by_id_string_.remove "$id"
    store_
    return id

  store_ -> none:
    entries := entry_by_id_string_.map: | _ entry/List | entry[0..2]
    flash_[KEY_] = entries
