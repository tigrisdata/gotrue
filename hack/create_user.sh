#!/bin/sh

CREATE=${CREATE_SUPER_ADMIN_USER:false}
if [ "${CREATE}" == "true" ]; then
  gotrue admin --instance_id="$GOTRUE_INSTANCE_ID" --aud="$GOTRUE_SUPERADMIN_AUD" --superadmin=true createuser "$GOTRUE_SUPERADMIN_USERNAME" "$GOTRUE_SUPERADMIN_PASSWORD" || true
else
  echo "Skipped creation of user"
fi
