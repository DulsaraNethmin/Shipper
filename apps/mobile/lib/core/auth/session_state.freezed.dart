// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'session_state.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$SessionState {





@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is SessionState);
}


@override
int get hashCode => runtimeType.hashCode;

@override
String toString() {
  return 'SessionState()';
}


}

/// @nodoc
class $SessionStateCopyWith<$Res>  {
$SessionStateCopyWith(SessionState _, $Res Function(SessionState) __);
}


/// Adds pattern-matching-related methods to [SessionState].
extension SessionStatePatterns on SessionState {
/// A variant of `map` that fallback to returning `orElse`.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case _:
///     return orElse();
/// }
/// ```

@optionalTypeArgs TResult maybeMap<TResult extends Object?>({TResult Function( SessionRestoring value)?  restoring,TResult Function( SessionSignedOut value)?  signedOut,TResult Function( SessionSignedIn value)?  signedIn,required TResult orElse(),}){
final _that = this;
switch (_that) {
case SessionRestoring() when restoring != null:
return restoring(_that);case SessionSignedOut() when signedOut != null:
return signedOut(_that);case SessionSignedIn() when signedIn != null:
return signedIn(_that);case _:
  return orElse();

}
}
/// A `switch`-like method, using callbacks.
///
/// Callbacks receives the raw object, upcasted.
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case final Subclass2 value:
///     return ...;
/// }
/// ```

@optionalTypeArgs TResult map<TResult extends Object?>({required TResult Function( SessionRestoring value)  restoring,required TResult Function( SessionSignedOut value)  signedOut,required TResult Function( SessionSignedIn value)  signedIn,}){
final _that = this;
switch (_that) {
case SessionRestoring():
return restoring(_that);case SessionSignedOut():
return signedOut(_that);case SessionSignedIn():
return signedIn(_that);}
}
/// A variant of `map` that fallback to returning `null`.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case _:
///     return null;
/// }
/// ```

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>({TResult? Function( SessionRestoring value)?  restoring,TResult? Function( SessionSignedOut value)?  signedOut,TResult? Function( SessionSignedIn value)?  signedIn,}){
final _that = this;
switch (_that) {
case SessionRestoring() when restoring != null:
return restoring(_that);case SessionSignedOut() when signedOut != null:
return signedOut(_that);case SessionSignedIn() when signedIn != null:
return signedIn(_that);case _:
  return null;

}
}
/// A variant of `when` that fallback to an `orElse` callback.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case _:
///     return orElse();
/// }
/// ```

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>({TResult Function()?  restoring,TResult Function()?  signedOut,TResult Function()?  signedIn,required TResult orElse(),}) {final _that = this;
switch (_that) {
case SessionRestoring() when restoring != null:
return restoring();case SessionSignedOut() when signedOut != null:
return signedOut();case SessionSignedIn() when signedIn != null:
return signedIn();case _:
  return orElse();

}
}
/// A `switch`-like method, using callbacks.
///
/// As opposed to `map`, this offers destructuring.
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case Subclass2(:final field2):
///     return ...;
/// }
/// ```

@optionalTypeArgs TResult when<TResult extends Object?>({required TResult Function()  restoring,required TResult Function()  signedOut,required TResult Function()  signedIn,}) {final _that = this;
switch (_that) {
case SessionRestoring():
return restoring();case SessionSignedOut():
return signedOut();case SessionSignedIn():
return signedIn();}
}
/// A variant of `when` that fallback to returning `null`
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case _:
///     return null;
/// }
/// ```

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>({TResult? Function()?  restoring,TResult? Function()?  signedOut,TResult? Function()?  signedIn,}) {final _that = this;
switch (_that) {
case SessionRestoring() when restoring != null:
return restoring();case SessionSignedOut() when signedOut != null:
return signedOut();case SessionSignedIn() when signedIn != null:
return signedIn();case _:
  return null;

}
}

}

/// @nodoc


class SessionRestoring implements SessionState {
  const SessionRestoring();
  






@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is SessionRestoring);
}


@override
int get hashCode => runtimeType.hashCode;

@override
String toString() {
  return 'SessionState.restoring()';
}


}




/// @nodoc


class SessionSignedOut implements SessionState {
  const SessionSignedOut();
  






@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is SessionSignedOut);
}


@override
int get hashCode => runtimeType.hashCode;

@override
String toString() {
  return 'SessionState.signedOut()';
}


}




/// @nodoc


class SessionSignedIn implements SessionState {
  const SessionSignedIn();
  






@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is SessionSignedIn);
}


@override
int get hashCode => runtimeType.hashCode;

@override
String toString() {
  return 'SessionState.signedIn()';
}


}




// dart format on
