// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import monitor

class PriorityQueueElement:
  value/any
  priority/int
  position_/int := -1

  constructor .value --.priority:

  is-linked -> bool:
    return position_ != -1

/**
A simple priority queue implementation.

Uses a heap to store the elements.
Each element knows its position in the heap, so that we can update the heap in O(log n) time.
*/
class PriorityQueue:
  heap_/List ::= []

  size -> int:
    return heap_.size

  add element/PriorityQueueElement -> none:
    heap_.add element
    element.position_ = heap_.size - 1
    bubble-up_ (heap_.size - 1)

  remove element/PriorityQueueElement -> none:
    index := element.position_
    // Swap the element with the last element.
    heap_[index] = heap_.last
    heap_.resize (heap_.size - 1)
    // Move the element down the tree.
    bubble-down_ index
    element.position_ = -1

  first -> PriorityQueueElement:
    return heap_.first

  bubble-up_ index/int:
    heap := heap_
    while index > 0:
      parent-index := (index - 1) / 2
      current := heap[index]
      parent := heap[parent-index]
      if current.priority < parent.priority:
        // Swap the elements.
        heap[index] = parent
        parent.position_ = index
        heap[parent-index] = current
        current.position_ = parent-index
        // Move up the tree.
        index = parent-index
      else:
        break

  bubble-down_ index/int:
    heap := heap_
    if index >= heap.size: return
    while true:
      current := heap[index]
      left-index := 2 * index + 1
      right-index := 2 * index + 2
      if left-index >= heap.size:
        break
      smallest-index := left-index
      if right-index < heap.size and heap[right-index].priority < heap[left-index].priority:
        smallest-index = right-index
      if current.priority < heap[smallest-index].priority:
        break
      // Swap the elements.
      heap[index] = heap[smallest-index]
      heap[index].position_ = index
      heap[smallest-index] = current
      current.position_ = smallest-index
      // Move down the tree.
      index = smallest-index

class ScheduledCallbacks:
  queue_/PriorityQueue? := null
  task_/Task? := null
  signal_/monitor.Signal ::= monitor.Signal

  /**
  Schedules the given $callback to run in $duration time.
  Returns a token that can be used to cancel the scheduled callback.
  */
  schedule duration/Duration callback/Lambda -> any:
    if queue_ == null:
      queue_ = PriorityQueue
    deadline := Time.monotonic-us + duration.in-us
    element := PriorityQueueElement --priority=deadline callback
    queue_.add element
    if task_ == null:
      watch-deadlines_
    else:
      signal_.raise

    return element

  /**
  Cancels the scheduled callback identified by the given $token.

  The $token must have beenn returned by a previous call to $schedule.
  Callbacks can be canceled multiple times, but only the first call has an effect.
  */
  cancel token/any:
    element := token as PriorityQueueElement
    if not element.is-linked: return
    queue_.remove element

  watch-deadlines_:
    task_ = task::
      try:
        while true:
          element/PriorityQueueElement? := null
          now := Time.monotonic-us
          while queue_.size > 0 and queue_.first.priority <= now:
            element = queue_.first
            queue_.remove element
            catch --trace:
              callback := element.value as Lambda
              callback.call
          if queue_.size == 0:
            break
          element = queue_.first
          assert: element and element.priority > now
          // Wait for the next deadline or a change in the queue.
          catch --unwind=(: it != DEADLINE-EXCEEDED-ERROR):
            with-timeout --us=(element.priority - now): signal_.wait
      finally:
        queue_ = null
        task_ = null
