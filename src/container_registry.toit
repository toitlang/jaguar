// Copyright (C) 2022 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import device
import uuid
import system.containers

flash_  ::= device.FlashStore
jaguar_ / uuid.Uuid ::= containers.current

class ContainerRegistry:
  static KEY_ /string ::= "jag.containers"

  loaded_     / bool := false
  id_by_name_ / Map ::= {:}  // Map<string, uuid.Uuid>
  name_by_id_ / Map ::= {:}  // Map<uuid.Uuid, string>

  entries -> Map:
    ensure_loaded_
    result := {:}
    name_by_id_.do: | id/uuid.Uuid name/string | result["$id"] = name
    return result

  install name/string? [block] -> uuid.Uuid:
    ensure_loaded_
    // Uninstall all unnamed images. This is used to prepare
    // for running another unnamed image.
    images/List ::= containers.images
    images.do: | id/uuid.Uuid |
      if not name_by_id_.contains id:
        containers.uninstall id
    if name: uninstall name
    // Now actually create the image by invoking the block.
    image := block.call
    if not name: return image
    // Update the name mapping and make sure we do not have
    // and old name for the same image floating around.
    old := name_by_id_.get image
    if old: id_by_name_.remove old
    name_by_id_[image] = name
    id_by_name_[name] = image
    store_
    return image

  uninstall name/string -> uuid.Uuid?:
    ensure_loaded_
    id := id_by_name_.get name
    if not id: return null
    containers.uninstall id
    id_by_name_.remove name
    name_by_id_.remove id
    store_
    return id

  ensure_loaded_ -> none:
    if loaded_: return
    dirty := true
    entries := {:}
    catch --trace:
      entries = flash_.get KEY_
      dirty = false
    // Run through the images actually installed in flash and update the
    // registry accordingly. This involves inventing names for unexpected
    // containers found in flash and pruning names for containers that
    // we cannot find anymore.
    index := 0
    images/List ::= containers.images
    images.do: | id/uuid.Uuid |
      name/string? := null
      catch: name = entries.get "$id"
      if not name:
        name = (id == jaguar_) ? "jaguar" : "container-$(index++)"
        dirty = true
      id_by_name_[name] = id
      name_by_id_[id] = name
    // We're done loading. If we've changed the name mapping in any way,
    // we write the updated entries back into flash.
    loaded_ = true
    if dirty or entries.size > images.size: store_

  store_ -> none:
    flash_.set KEY_ entries
